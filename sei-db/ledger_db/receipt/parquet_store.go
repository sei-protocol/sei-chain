package receipt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

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

	reader           *parquetReader
	cache            *ledgerCache
	storeKey         sdk.StoreKey
	wal              dbwal.GenericWAL[parquetWALEntry]
	latestVersion    atomic.Int64
	earliestVersion  atomic.Int64
	currentFileStart atomic.Uint64
	closeOnce        sync.Once
}

func newParquetReceiptStore(log dbLogger.Logger, cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if err := os.MkdirAll(cfg.DBDirectory, 0o755); err != nil {
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
		cache:          newLedgerCache(),
		storeKey:       storeKey,
		wal:            receiptWAL,
	}

	if maxBlock, ok, err := reader.maxReceiptBlockNumber(context.Background()); err != nil {
		return nil, err
	} else if ok {
		store.latestVersion.Store(int64(maxBlock))
		if maxBlock < ^uint64(0) {
			store.fileStartBlock = maxBlock + 1
		}
	}

	if reader.closedReceiptFileCount() == 0 {
		store.fileStartBlock = 0
	}
	store.currentFileStart.Store(store.fileStartBlock)

	if err := store.initWriters(); err != nil {
		return nil, err
	}

	if err := store.replayWAL(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *parquetReceiptStore) LatestVersion() int64 {
	if s == nil {
		return 0
	}
	return s.latestVersion.Load()
}

func (s *parquetReceiptStore) SetLatestVersion(version int64) error {
	if s == nil {
		return ErrNotConfigured
	}
	s.latestVersion.Store(version)
	return nil
}

func (s *parquetReceiptStore) SetEarliestVersion(version int64) error {
	if s == nil {
		return ErrNotConfigured
	}
	s.earliestVersion.Store(version)
	return nil
}

func (s *parquetReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}

	if s.cache != nil {
		if receipt, ok := s.cache.GetReceipt(txHash); ok {
			return receipt, nil
		}
	}

	if s.reader != nil {
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
	if s == nil {
		return nil, ErrNotConfigured
	}

	if s.cache != nil {
		if receipt, ok := s.cache.GetReceipt(txHash); ok {
			return receipt, nil
		}
	}

	if s.reader == nil {
		return nil, ErrNotFound
	}

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
	if s == nil {
		return ErrNotConfigured
	}

	if len(receipts) == 0 {
		return nil
	}

	blockHash := common.Hash{}

	inputs := make([]parquetReceiptInput, 0, len(receipts))
	cacheBatches := make([]cacheBatch, 0)
	walEntries := make([]parquetWALEntry, 0, len(receipts))

	var (
		currentBlock  uint64
		logStartIndex uint
		cacheEntries  []receiptCacheEntry
		cacheLogs     []*ethtypes.Log
		maxBlock      uint64
	)

	flushCacheBatch := func(blockNumber uint64) {
		if len(cacheEntries) == 0 && len(cacheLogs) == 0 {
			return
		}
		cacheBatches = append(cacheBatches, cacheBatch{
			blockNumber: blockNumber,
			entries:     cacheEntries,
			logs:        cacheLogs,
		})
		cacheEntries = nil
		cacheLogs = nil
	}

	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}

		receipt := record.Receipt
		blockNumber := receipt.BlockNumber
		if blockNumber > maxBlock {
			maxBlock = blockNumber
		}

		if currentBlock == 0 {
			currentBlock = blockNumber
		}
		if blockNumber != currentBlock {
			flushCacheBatch(currentBlock)
			currentBlock = blockNumber
			logStartIndex = 0
		}

		receiptBytes, err := receipt.Marshal()
		if err != nil {
			return err
		}
		walEntries = append(walEntries, parquetWALEntry{
			ReceiptBytes: copyBytesOrEmpty(receiptBytes),
		})

		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquetReceiptInput{
			blockNumber: blockNumber,
			receipt: receiptRecord{
				TxHash:       copyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: copyBytesOrEmpty(receiptBytes),
			},
			logs: buildLogRecords(txLogs, blockHash),
		})

		cacheEntries = append(cacheEntries, receiptCacheEntry{
			TxHash:  record.TxHash,
			Receipt: receipt,
		})
		cacheLogs = append(cacheLogs, txLogs...)
	}
	flushCacheBatch(currentBlock)

	if s.wal != nil {
		for i := range walEntries {
			if err := s.wal.Write(walEntries[i]); err != nil {
				return err
			}
		}
	}

	s.mu.Lock()
	for i := range inputs {
		if err := s.applyReceiptLocked(inputs[i]); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()

	if maxBlock > 0 {
		s.latestVersion.Store(int64(maxBlock))
	}

	if s.cache != nil {
		for i := range cacheBatches {
			s.cache.AddReceiptsBatch(cacheBatches[i].blockNumber, cacheBatches[i].entries)
			s.cache.AddLogsForBlock(cacheBatches[i].blockNumber, cacheBatches[i].logs)
		}
	}

	return nil
}

func (s *parquetReceiptStore) FilterLogs(ctx sdk.Context, blockHeight int64, blockHash common.Hash, txHashes []common.Hash, crit filters.FilterCriteria, applyExactMatch bool) ([]*ethtypes.Log, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if len(txHashes) == 0 {
		return []*ethtypes.Log{}, nil
	}

	blockNumber := uint64(blockHeight)
	if applyExactMatch {
		if s.cache != nil && s.useCacheForBlock(blockNumber) {
			logs := s.cache.GetLogsWithFilter(blockNumber, blockNumber, crit.Addresses, crit.Topics)
			for _, lg := range logs {
				lg.BlockHash = blockHash
				lg.BlockNumber = blockNumber
			}
			return logs, nil
		}

		if s.reader != nil && !s.useCacheForBlock(blockNumber) {
			filter := logFilter{
				FromBlock: &blockNumber,
				ToBlock:   &blockNumber,
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
					BlockHash:   blockHash,
				}
				copy(logEntry.Address[:], lr.Address)
				logEntry.Topics = buildTopicsFromLogResult(lr)
				logEntry.BlockNumber = blockNumber
				logs = append(logs, logEntry)
			}
			return logs, nil
		}
	}

	return s.filterLogsFromReceipts(ctx, blockHeight, blockHash, txHashes, crit, applyExactMatch)
}

func (s *parquetReceiptStore) Close() error {
	if s == nil {
		return nil
	}

	var err error
	s.closeOnce.Do(func() {
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
		if s.wal != nil {
			if closeErr := s.wal.Close(); closeErr != nil {
				err = closeErr
				return
			}
		}
		if s.reader != nil {
			if closeErr := s.reader.Close(); closeErr != nil {
				err = closeErr
			}
		}
	})

	return err
}

type parquetReceiptInput struct {
	blockNumber uint64
	receipt     receiptRecord
	logs        []logRecord
}

type cacheBatch struct {
	blockNumber uint64
	entries     []receiptCacheEntry
	logs        []*ethtypes.Log
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

	if s.reader != nil {
		s.reader.onFileRotation(oldStartBlock)
	}
	if s.cache != nil {
		s.cache.Rotate()
	}
	s.clearWAL()

	s.fileStartBlock = newBlockNumber
	s.currentFileStart.Store(newBlockNumber)
	s.blocksInFile = 0

	return s.initWriters()
}

func (s *parquetReceiptStore) initWriters() error {
	receiptPath := filepath.Join(s.basePath, fmt.Sprintf("receipts_%d.parquet", s.fileStartBlock))
	logPath := filepath.Join(s.basePath, fmt.Sprintf("logs_%d.parquet", s.fileStartBlock))

	receiptFile, err := os.Create(receiptPath)
	if err != nil {
		return fmt.Errorf("failed to create receipt parquet file: %w", err)
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		receiptFile.Close()
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

func (s *parquetReceiptStore) useCacheForBlock(blockNumber uint64) bool {
	current := s.currentFileStart.Load()
	if current == 0 {
		return true
	}
	return blockNumber >= current
}

func (s *parquetReceiptStore) filterLogsFromReceipts(ctx sdk.Context, blockHeight int64, blockHash common.Hash, txHashes []common.Hash, crit filters.FilterCriteria, applyExactMatch bool) ([]*ethtypes.Log, error) {
	hasFilters := len(crit.Addresses) != 0 || len(crit.Topics) != 0
	var filterIndexes [][]bloomIndexes
	if hasFilters {
		filterIndexes = encodeFilters(crit.Addresses, crit.Topics)
	}

	logs := make([]*ethtypes.Log, 0)
	totalLogs := uint(0)
	evmTxIndex := 0

	for _, txHash := range txHashes {
		receipt, err := s.GetReceipt(ctx, txHash)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("collectLogs: unable to find receipt for hash %s", txHash.Hex()))
			continue
		}

		txLogs := getLogsForTx(receipt, totalLogs)

		if hasFilters {
			if len(receipt.LogsBloom) == 0 || matchFilters(ethtypes.Bloom(receipt.LogsBloom), filterIndexes) {
				if applyExactMatch {
					for _, log := range txLogs {
						log.TxIndex = uint(evmTxIndex)        //nolint:gosec
						log.BlockNumber = uint64(blockHeight) //nolint:gosec
						log.BlockHash = blockHash
						if isLogExactMatch(log, crit) {
							logs = append(logs, log)
						}
					}
				} else {
					for _, log := range txLogs {
						log.TxIndex = uint(evmTxIndex)        //nolint:gosec
						log.BlockNumber = uint64(blockHeight) //nolint:gosec
						log.BlockHash = blockHash
						logs = append(logs, log)
					}
				}
			}
		} else {
			for _, log := range txLogs {
				log.TxIndex = uint(evmTxIndex)        //nolint:gosec
				log.BlockNumber = uint64(blockHeight) //nolint:gosec
				log.BlockHash = blockHash
				logs = append(logs, log)
			}
		}

		totalLogs += uint(len(txLogs))
		evmTxIndex++
	}

	return logs, nil
}

func (s *parquetReceiptStore) replayWAL() error {
	if s.wal == nil {
		return nil
	}

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
		currentBlock  uint64
		logStartIndex uint
		cacheEntries  []receiptCacheEntry
		cacheLogs     []*ethtypes.Log
		maxBlock      uint64
		dropOffset    uint64
	)

	flushCacheBatch := func(blockNumber uint64) {
		if s.cache == nil || len(cacheEntries) == 0 {
			cacheEntries = nil
			cacheLogs = nil
			return
		}
		s.cache.AddReceiptsBatch(blockNumber, cacheEntries)
		if len(cacheLogs) > 0 {
			s.cache.AddLogsForBlock(blockNumber, cacheLogs)
		}
		cacheEntries = nil
		cacheLogs = nil
	}

	blockHash := common.Hash{}
	fileStartBlock := s.fileStartBlock

	err := s.wal.Replay(firstOffset, lastOffset, func(offset uint64, entry parquetWALEntry) error {
		if len(entry.ReceiptBytes) == 0 {
			return nil
		}

		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(entry.ReceiptBytes); err != nil {
			return err
		}

		blockNumber := receipt.BlockNumber
		if blockNumber < fileStartBlock {
			dropOffset = offset
			return nil
		}

		txHash := common.HexToHash(receipt.TxHashHex)

		if currentBlock == 0 {
			currentBlock = blockNumber
		}
		if blockNumber != currentBlock {
			flushCacheBatch(currentBlock)
			currentBlock = blockNumber
			logStartIndex = 0
		}

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
				ReceiptBytes: copyBytesOrEmpty(entry.ReceiptBytes),
			},
			logs: buildLogRecords(txLogs, blockHash),
		}

		if err := s.applyReceiptLocked(input); err != nil {
			return err
		}

		if s.cache != nil {
			cacheEntries = append(cacheEntries, receiptCacheEntry{
				TxHash:  txHash,
				Receipt: receipt,
			})
			cacheLogs = append(cacheLogs, txLogs...)
		}

		if blockNumber > maxBlock {
			maxBlock = blockNumber
		}

		return nil
	})
	if err != nil {
		return err
	}

	flushCacheBatch(currentBlock)

	if maxBlock > 0 {
		s.latestVersion.Store(int64(maxBlock))
	}
	if dropOffset > 0 {
		_ = s.wal.TruncateBefore(dropOffset + 1)
	}
	return nil
}

func (s *parquetReceiptStore) clearWAL() {
	if s.wal == nil {
		return
	}
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
			TxIndex:     uint32(lg.TxIndex),
			LogIndex:    uint32(lg.Index),
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
