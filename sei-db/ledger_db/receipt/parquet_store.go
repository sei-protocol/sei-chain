//go:build duckdb
// +build duckdb

package receipt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/parquet-go/parquet-go"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	maxInt64  = int64(^uint64(0) >> 1)
	maxUint32 = ^uint32(0)
)

type receiptRecord struct {
	TxHash       []byte `parquet:"tx_hash"`
	BlockNumber  uint64 `parquet:"block_number"`
	ReceiptBytes []byte `parquet:"receipt_bytes"`
}

type logRecord struct {
	BlockNumber uint64 `parquet:"block_number"`
	TxHash      []byte `parquet:"tx_hash"`
	TxIndex     uint32 `parquet:"tx_index"`
	LogIndex    uint32 `parquet:"log_index"`
	Address     []byte `parquet:"address"`
	BlockHash   []byte `parquet:"block_hash"`
	Removed     bool   `parquet:"removed"`

	Topic0 []byte `parquet:"topic0"`
	Topic1 []byte `parquet:"topic1"`
	Topic2 []byte `parquet:"topic2"`
	Topic3 []byte `parquet:"topic3"`

	Data []byte `parquet:"data"`
}

type parquetStoreConfig struct {
	BlockFlushInterval uint64
	MaxBlocksPerFile   uint64
}

func defaultParquetStoreConfig() parquetStoreConfig {
	return parquetStoreConfig{
		BlockFlushInterval: 1,
		MaxBlocksPerFile:   500,
	}
}

type parquetReceiptStore struct {
	basePath      string
	receiptWriter *parquet.GenericWriter[receiptRecord]
	logWriter     *parquet.GenericWriter[logRecord]
	receiptFile   *os.File
	logFile       *os.File

	mu               sync.Mutex
	fileStartBlock   uint64
	receiptsBuffer   []receiptRecord
	logsBuffer       []logRecord
	config           parquetStoreConfig
	lastSeenBlock    uint64
	blocksSinceFlush uint64
	blocksInFile     uint64

	reader             *parquetReader
	storeKey           sdk.StoreKey
	wal                dbwal.GenericWAL[parquetWALEntry]
	latestVersion      atomic.Int64
	earliestVersion    atomic.Int64
	warmupCacheRecords []ReceiptRecord
	closeOnce          sync.Once

	log        dbLogger.Logger
	keepRecent int64
	pruneStop  chan struct{}
}

func newParquetReceiptStore(log dbLogger.Logger, cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create parquet base directory: %w", err)
	}

	reader, err := newParquetReader(cfg.DBDirectory)
	if err != nil {
		return nil, err
	}

	walDir := filepath.Join(cfg.DBDirectory, "parquet-wal")
	receiptWAL, err := newParquetWAL(log, walDir)
	if err != nil {
		return nil, err
	}

	store := &parquetReceiptStore{
		basePath:       cfg.DBDirectory,
		receiptsBuffer: make([]receiptRecord, 0, 1000),
		logsBuffer:     make([]logRecord, 0, 10000),
		config:         defaultParquetStoreConfig(),
		reader:         reader,
		storeKey:       storeKey,
		wal:            receiptWAL,
		log:            log,
		keepRecent:     int64(cfg.KeepRecent),
		pruneStop:      make(chan struct{}),
	}

	if maxBlock, ok, err := reader.maxReceiptBlockNumber(context.Background()); err != nil {
		return nil, err
	} else if ok {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return nil, err
		}
		store.latestVersion.Store(latest)
		if maxBlock < ^uint64(0) {
			store.fileStartBlock = maxBlock + 1
		}
	}

	if reader.closedReceiptFileCount() == 0 {
		store.fileStartBlock = 0
	}

	if err := store.initWriters(); err != nil {
		return nil, err
	}

	if err := store.replayWAL(); err != nil {
		return nil, err
	}

	store.startPruning(int64(cfg.PruneIntervalSeconds))

	return store, nil
}

func (s *parquetReceiptStore) LatestVersion() int64 {
	return s.latestVersion.Load()
}

func (s *parquetReceiptStore) SetLatestVersion(version int64) error {
	s.latestVersion.Store(version)
	return nil
}

func (s *parquetReceiptStore) SetEarliestVersion(version int64) error {
	s.earliestVersion.Store(version)
	return nil
}

func (s *parquetReceiptStore) cacheRotateInterval() uint64 {
	return s.config.MaxBlocksPerFile
}

func (s *parquetReceiptStore) warmupReceipts() []ReceiptRecord {
	records := s.warmupCacheRecords
	s.warmupCacheRecords = nil
	return records
}

func (s *parquetReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.reader.getReceiptByTxHash(ctx.Context(), txHash)
	if err != nil {
		return nil, err
	}
	if result != nil {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(result.ReceiptBytes); err != nil {
			return nil, err
		}
		return receipt, nil
	}

	store := ctx.KVStore(s.storeKey)
	bz := store.Get(types.ReceiptKey(txHash))
	if bz == nil {
		return nil, ErrNotFound
	}
	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *parquetReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.reader.getReceiptByTxHash(ctx.Context(), txHash)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrNotFound
	}

	receipt := &types.Receipt{}
	if err := receipt.Unmarshal(result.ReceiptBytes); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *parquetReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	if len(receipts) == 0 {
		if ctx.BlockHeight() > s.latestVersion.Load() {
			s.latestVersion.Store(ctx.BlockHeight())
		}
		return nil
	}

	blockHash := common.Hash{}

	// Group receipts by block for batched WAL writes
	type blockBatch struct {
		blockNumber  uint64
		receiptBytes [][]byte
		inputs       []parquetReceiptInput
	}

	var (
		currentBlock  uint64
		logStartIndex uint
		maxBlock      uint64
		currentBatch  *blockBatch
		batches       []*blockBatch
	)

	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}

		receipt := record.Receipt
		blockNumber := receipt.BlockNumber
		if blockNumber > maxBlock {
			maxBlock = blockNumber
		}

		// Start new batch when block changes
		if currentBatch == nil || blockNumber != currentBlock {
			if currentBatch != nil {
				batches = append(batches, currentBatch)
			}
			currentBatch = &blockBatch{
				blockNumber:  blockNumber,
				receiptBytes: make([][]byte, 0, 100),
				inputs:       make([]parquetReceiptInput, 0, 100),
			}
			currentBlock = blockNumber
			logStartIndex = 0
		}

		receiptBytes := record.ReceiptBytes
		if len(receiptBytes) == 0 {
			var err error
			receiptBytes, err = receipt.Marshal()
			if err != nil {
				return err
			}
		}
		currentBatch.receiptBytes = append(currentBatch.receiptBytes, receiptBytes)

		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		currentBatch.inputs = append(currentBatch.inputs, parquetReceiptInput{
			blockNumber: blockNumber,
			receipt: receiptRecord{
				TxHash:       copyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: receiptBytes,
			},
			logs: buildLogRecords(txLogs, blockHash),
		})
	}

	// Add final batch
	if currentBatch != nil {
		batches = append(batches, currentBatch)
	}

	// Write one WAL entry per block (batched)
	for _, batch := range batches {
		walEntry := parquetWALEntry{
			BlockNumber: batch.blockNumber,
			Receipts:    batch.receiptBytes,
		}
		if err := s.wal.Write(walEntry); err != nil {
			return err
		}
	}

	// Apply to parquet buffers
	s.mu.Lock()
	for _, batch := range batches {
		for i := range batch.inputs {
			if err := s.applyReceiptLocked(batch.inputs[i]); err != nil {
				s.mu.Unlock()
				return err
			}
		}
	}
	s.mu.Unlock()

	if maxBlock > 0 {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return err
		}
		// Only update latestVersion if the new value is higher (avoids lowering it when writing receipts out of order)
		if latest > s.latestVersion.Load() {
			s.latestVersion.Store(latest)
		}
	}

	return nil
}

// FilterLogs queries logs across a range of blocks using a single DuckDB query.
// For single-block queries, set fromBlock == toBlock.
func (s *parquetReceiptStore) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}

	filter := logFilter{
		FromBlock: &fromBlock,
		ToBlock:   &toBlock,
		Addresses: crit.Addresses,
		Topics:    crit.Topics,
	}

	results, err := s.reader.getLogs(ctx.Context(), filter)
	if err != nil {
		return nil, err
	}

	logs := make([]*ethtypes.Log, 0, len(results))
	for i := range results {
		lr := results[i]
		logEntry := &ethtypes.Log{
			BlockNumber: lr.BlockNumber,
			TxHash:      common.BytesToHash(lr.TxHash),
			TxIndex:     uint(lr.TxIndex),
			Index:       uint(lr.LogIndex),
			Data:        lr.Data,
			Removed:     lr.Removed,
			BlockHash:   common.BytesToHash(lr.BlockHash),
		}
		copy(logEntry.Address[:], lr.Address)
		logEntry.Topics = buildTopicsFromLogResult(lr)
		logs = append(logs, logEntry)
	}

	return logs, nil
}

func (s *parquetReceiptStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.pruneStop != nil {
			close(s.pruneStop)
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		if flushErr := s.flushLocked(); flushErr != nil {
			err = flushErr
			return
		}
		if closeErr := s.closeWritersLocked(); closeErr != nil {
			err = closeErr
			return
		}
		if closeErr := s.wal.Close(); closeErr != nil {
			err = closeErr
			return
		}
		if closeErr := s.reader.Close(); closeErr != nil {
			err = closeErr
		}
	})

	return err
}

func (s *parquetReceiptStore) startPruning(pruneIntervalSeconds int64) {
	if s.keepRecent <= 0 || pruneIntervalSeconds <= 0 {
		return
	}
	go func() {
		for {
			latestVersion := s.latestVersion.Load()
			pruneBeforeBlock := latestVersion - s.keepRecent
			if pruneBeforeBlock > 0 {
				pruned := s.pruneOldFiles(uint64(pruneBeforeBlock))
				if pruned > 0 && s.log != nil {
					s.log.Info(fmt.Sprintf("Pruned %d parquet file pairs older than block %d", pruned, pruneBeforeBlock))
				}
			}

			// Add jitter to avoid thundering herd
			jitter := time.Duration(float64(pruneIntervalSeconds)*0.5) * time.Second
			sleepDuration := time.Duration(pruneIntervalSeconds)*time.Second + jitter

			select {
			case <-s.pruneStop:
				return
			case <-time.After(sleepDuration):
				// Continue to next iteration
			}
		}
	}()
}

func (s *parquetReceiptStore) pruneOldFiles(pruneBeforeBlock uint64) int {
	prunedCount := 0

	// Get list of files to prune from the reader
	filesToPrune := s.reader.getFilesBeforeBlock(pruneBeforeBlock)

	for _, filePair := range filesToPrune {
		// Remove receipt file
		if filePair.receiptFile != "" {
			if err := os.Remove(filePair.receiptFile); err != nil && !os.IsNotExist(err) {
				if s.log != nil {
					s.log.Error("failed to prune receipt file", "file", filePair.receiptFile, "err", err)
				}
				continue
			}
		}
		// Remove log file
		if filePair.logFile != "" {
			if err := os.Remove(filePair.logFile); err != nil && !os.IsNotExist(err) {
				if s.log != nil {
					s.log.Error("failed to prune log file", "file", filePair.logFile, "err", err)
				}
				continue
			}
		}
		prunedCount++
	}

	// Update reader's file lists
	if prunedCount > 0 {
		s.reader.removeFilesBeforeBlock(pruneBeforeBlock)
	}

	return prunedCount
}

type parquetReceiptInput struct {
	blockNumber uint64
	receipt     receiptRecord
	logs        []logRecord
}

func (s *parquetReceiptStore) applyReceiptLocked(input parquetReceiptInput) error {
	blockNumber := input.blockNumber
	isNewBlock := blockNumber != s.lastSeenBlock
	if isNewBlock {
		if s.lastSeenBlock != 0 {
			s.blocksSinceFlush++
			s.blocksInFile++
		}
		s.lastSeenBlock = blockNumber
	}

	s.receiptsBuffer = append(s.receiptsBuffer, input.receipt)
	if len(input.logs) > 0 {
		s.logsBuffer = append(s.logsBuffer, input.logs...)
	}

	if s.config.BlockFlushInterval > 0 && s.blocksSinceFlush >= s.config.BlockFlushInterval {
		if err := s.flushLocked(); err != nil {
			return err
		}
		s.blocksSinceFlush = 0
	}

	if isNewBlock && s.shouldRotateFile() {
		if err := s.rotateFileLocked(blockNumber); err != nil {
			return err
		}
	}

	return nil
}

func (s *parquetReceiptStore) shouldRotateFile() bool {
	if s.config.MaxBlocksPerFile > 0 && s.blocksInFile >= s.config.MaxBlocksPerFile {
		return true
	}
	return false
}

func (s *parquetReceiptStore) rotateFileLocked(newBlockNumber uint64) error {
	if err := s.flushLocked(); err != nil {
		return err
	}

	oldStartBlock := s.fileStartBlock
	if err := s.closeWritersLocked(); err != nil {
		return err
	}

	s.reader.onFileRotation(oldStartBlock)
	s.clearWAL()

	s.fileStartBlock = newBlockNumber
	s.blocksInFile = 0

	return s.initWriters()
}

func (s *parquetReceiptStore) initWriters() error {
	receiptPath := filepath.Join(s.basePath, fmt.Sprintf("receipts_%d.parquet", s.fileStartBlock))
	logPath := filepath.Join(s.basePath, fmt.Sprintf("logs_%d.parquet", s.fileStartBlock))

	// #nosec G304 -- paths are constructed from configured base directory
	receiptFile, err := os.Create(receiptPath)
	if err != nil {
		return fmt.Errorf("failed to create receipt parquet file: %w", err)
	}

	// #nosec G304 -- paths are constructed from configured base directory
	logFile, err := os.Create(logPath)
	if err != nil {
		if closeErr := receiptFile.Close(); closeErr != nil {
			return fmt.Errorf("failed to create log parquet file: %w; close receipt file error: %v", err, closeErr)
		}
		return fmt.Errorf("failed to create log parquet file: %w", err)
	}

	blockNumberSorting := parquet.SortingWriterConfig(
		parquet.SortingColumns(parquet.Ascending("block_number")),
	)

	receiptWriter := parquet.NewGenericWriter[receiptRecord](receiptFile,
		parquet.Compression(&parquet.Snappy),
		blockNumberSorting,
	)
	logWriter := parquet.NewGenericWriter[logRecord](logFile,
		parquet.Compression(&parquet.Snappy),
		blockNumberSorting,
	)

	s.receiptFile = receiptFile
	s.logFile = logFile
	s.receiptWriter = receiptWriter
	s.logWriter = logWriter

	return nil
}

func (s *parquetReceiptStore) flushLocked() error {
	if len(s.receiptsBuffer) == 0 {
		return nil
	}

	if _, err := s.receiptWriter.Write(s.receiptsBuffer); err != nil {
		return fmt.Errorf("failed to write receipts to parquet: %w", err)
	}
	if err := s.receiptWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush receipt parquet writer: %w", err)
	}

	if len(s.logsBuffer) > 0 {
		if _, err := s.logWriter.Write(s.logsBuffer); err != nil {
			return fmt.Errorf("failed to write logs to parquet: %w", err)
		}
		if err := s.logWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush log parquet writer: %w", err)
		}
	}

	s.receiptsBuffer = s.receiptsBuffer[:0]
	s.logsBuffer = s.logsBuffer[:0]
	return nil
}

func (s *parquetReceiptStore) closeWritersLocked() error {
	var errs []error

	if s.receiptWriter != nil {
		if err := s.receiptWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt writer: %w", err))
		}
	}
	if s.logWriter != nil {
		if err := s.logWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log writer: %w", err))
		}
	}
	if s.receiptFile != nil {
		if err := s.receiptFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file: %w", err))
		}
	}
	if s.logFile != nil {
		if err := s.logFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log file: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

func (s *parquetReceiptStore) replayWAL() error {
	firstOffset, errFirst := s.wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := s.wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		maxBlock   uint64
		dropOffset uint64
	)

	blockHash := common.Hash{}
	fileStartBlock := s.fileStartBlock

	err := s.wal.Replay(firstOffset, lastOffset, func(offset uint64, entry parquetWALEntry) error {
		// Skip blocks older than current file
		if entry.BlockNumber < fileStartBlock {
			dropOffset = offset
			return nil
		}

		if len(entry.Receipts) == 0 {
			return nil
		}

		blockNumber := entry.BlockNumber
		var logStartIndex uint

		for _, receiptBytes := range entry.Receipts {
			if len(receiptBytes) == 0 {
				continue
			}

			receipt := &types.Receipt{}
			if err := receipt.Unmarshal(receiptBytes); err != nil {
				return err
			}

			txHash := common.HexToHash(receipt.TxHashHex)
			s.warmupCacheRecords = append(s.warmupCacheRecords, ReceiptRecord{
				TxHash:  txHash,
				Receipt: receipt,
			})

			txLogs := getLogsForTx(receipt, logStartIndex)
			logStartIndex += uint(len(txLogs))
			for _, lg := range txLogs {
				lg.BlockHash = blockHash
			}

			input := parquetReceiptInput{
				blockNumber: blockNumber,
				receipt: receiptRecord{
					TxHash:       copyBytes(txHash[:]),
					BlockNumber:  blockNumber,
					ReceiptBytes: receiptBytes,
				},
				logs: buildLogRecords(txLogs, blockHash),
			}

			if err := s.applyReceiptLocked(input); err != nil {
				return err
			}
		}

		if blockNumber > maxBlock {
			maxBlock = blockNumber
		}

		return nil
	})
	if err != nil {
		return err
	}

	if maxBlock > 0 {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return err
		}
		// Only update latestVersion if the new value is higher (avoids lowering it when writing receipts out of order)
		if latest > s.latestVersion.Load() {
			s.latestVersion.Store(latest)
		}
	}
	if dropOffset > 0 {
		_ = s.wal.TruncateBefore(dropOffset + 1)
	}
	return nil
}

func (s *parquetReceiptStore) clearWAL() {
	firstOffset, errFirst := s.wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return
	}
	lastOffset, errLast := s.wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return
	}
	if lastOffset < firstOffset {
		return
	}
	_ = s.wal.TruncateBefore(lastOffset + 1)
}

func buildLogRecords(logs []*ethtypes.Log, blockHash common.Hash) []logRecord {
	if len(logs) == 0 {
		return nil
	}

	records := make([]logRecord, 0, len(logs))
	for _, lg := range logs {
		topic0, topic1, topic2, topic3 := extractLogTopics(lg.Topics)
		rec := logRecord{
			BlockNumber: lg.BlockNumber,
			TxHash:      lg.TxHash[:],
			TxIndex:     uint32FromUint(lg.TxIndex),
			LogIndex:    uint32FromUint(lg.Index),
			Address:     lg.Address[:],
			BlockHash:   blockHash[:],
			Removed:     lg.Removed,
			Topic0:      topic0,
			Topic1:      topic1,
			Topic2:      topic2,
			Topic3:      topic3,
			Data:        lg.Data,
		}
		records = append(records, rec)
	}

	return records
}

func buildTopicsFromLogResult(lr logResult) []common.Hash {
	var topicList []common.Hash
	if len(lr.Topic0) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic0))
	}
	if len(lr.Topic1) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic1))
	}
	if len(lr.Topic2) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic2))
	}
	if len(lr.Topic3) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic3))
	}
	return topicList
}

func extractLogTopics(topics []common.Hash) ([]byte, []byte, []byte, []byte) {
	t0 := make([]byte, 0)
	t1 := make([]byte, 0)
	t2 := make([]byte, 0)
	t3 := make([]byte, 0)

	if len(topics) > 0 {
		t0 = make([]byte, common.HashLength)
		copy(t0, topics[0][:])
	}
	if len(topics) > 1 {
		t1 = make([]byte, common.HashLength)
		copy(t1, topics[1][:])
	}
	if len(topics) > 2 {
		t2 = make([]byte, common.HashLength)
		copy(t2, topics[2][:])
	}
	if len(topics) > 3 {
		t3 = make([]byte, common.HashLength)
		copy(t3, topics[3][:])
	}
	return t0, t1, t2, t3
}

func copyBytes(src []byte) []byte {
	if len(src) == 0 {
		return make([]byte, 0)
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func copyBytesOrEmpty(src []byte) []byte {
	if src == nil {
		return make([]byte, 0)
	}
	return copyBytes(src)
}

func int64FromUint64(value uint64) (int64, error) {
	if value > uint64(maxInt64) {
		return 0, fmt.Errorf("value %d overflows int64", value)
	}
	return int64(value), nil
}

func uint32FromUint(value uint) uint32 {
	if value > uint(maxUint32) {
		return maxUint32
	}
	return uint32(value)
}
