package receipt

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// parquetReceiptStore wraps the parquet.Store and implements ReceiptStore.
type parquetReceiptStore struct {
	store       *parquet.Store
	storeKey    sdk.StoreKey
	txHashIndex TxHashIndex
	indexPruner *txHashIndexPruner
}

func newParquetReceiptStore(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	txIndexBackend := dbconfig.NormalizeReceiptTxIndexBackend(cfg.TxIndexBackend)
	parquetTxIndexBackend := txIndexBackend
	if parquetTxIndexBackend == dbconfig.ReceiptTxIndexBackendNone {
		parquetTxIndexBackend = "none"
	}
	storeCfg := parquet.StoreConfig{
		DBDirectory:          cfg.DBDirectory,
		KeepRecent:           int64(cfg.KeepRecent),
		PruneIntervalSeconds: int64(cfg.PruneIntervalSeconds),
		TxIndexBackend:       parquetTxIndexBackend,
	}

	store, err := parquet.NewStore(storeCfg)
	if err != nil {
		return nil, err
	}

	wrapper := &parquetReceiptStore{
		store:    store,
		storeKey: storeKey,
	}

	switch txIndexBackend {
	case dbconfig.ReceiptTxIndexBackendNone:
	case dbconfig.ReceiptTxIndexBackendPebble:
		idx, err := NewPebbleTxHashIndex(TxHashIndexDir(cfg.DBDirectory))
		if err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("failed to open tx hash index: %w", err)
		}
		wrapper.txHashIndex = idx
		wrapper.indexPruner = newTxHashIndexPruner(
			idx,
			int64(cfg.KeepRecent),
			int64(cfg.PruneIntervalSeconds),
			func() int64 { return store.LatestVersion() },
		)
	default:
		_ = store.Close()
		return nil, fmt.Errorf("unsupported receipt tx index backend: %s", txIndexBackend)
	}

	if err := wrapper.replayWAL(); err != nil {
		_ = wrapper.Close()
		return nil, err
	}

	if wrapper.indexPruner != nil {
		wrapper.indexPruner.Start()
	}

	return wrapper, nil
}

func (s *parquetReceiptStore) LatestVersion() int64 {
	return s.store.LatestVersion()
}

func (s *parquetReceiptStore) SetLatestVersion(version int64) error {
	s.store.SetLatestVersion(version)
	return nil
}

func (s *parquetReceiptStore) SetEarliestVersion(version int64) error {
	s.store.SetEarliestVersion(version)
	return nil
}

func (s *parquetReceiptStore) cacheRotateInterval() uint64 {
	return s.store.CacheRotateInterval()
}

func (s *parquetReceiptStore) warmupReceipts() []ReceiptRecord {
	records := make([]ReceiptRecord, 0, len(s.store.WarmupRecords))
	for _, rec := range s.store.WarmupRecords {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(rec.ReceiptBytes); err != nil {
			continue
		}
		records = append(records, ReceiptRecord{
			TxHash:  common.BytesToHash(rec.TxHash),
			Receipt: receipt,
		})
	}
	s.store.WarmupRecords = nil
	return records
}

func (s *parquetReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.indexedReceiptLookup(ctx.Context(), txHash)
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

	if s.storeKey == nil {
		return nil, ErrNotFound
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
	result, err := s.indexedReceiptLookup(ctx.Context(), txHash)
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

// indexedReceiptLookup uses the tx hash index to narrow the parquet search to
// a single file. When the index is disabled the lookup returns
// ErrTxIndexDisabled instead of performing a full parquet scan, which would
// be prohibitively expensive at production scale.
func (s *parquetReceiptStore) indexedReceiptLookup(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	if s.txHashIndex == nil {
		return nil, ErrTxIndexDisabled
	}
	blockNum, ok, err := s.txHashIndex.GetBlockNumber(ctx, txHash)
	if err != nil {
		logger.Error("tx hash index lookup failed, falling back to full scan", "err", err)
		return s.store.GetReceiptByTxHash(ctx, txHash)
	}
	if !ok {
		return s.store.GetReceiptByTxHash(ctx, txHash)
	}
	return s.store.GetReceiptByTxHashInBlock(ctx, txHash, blockNum)
}

func (s *parquetReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	if len(receipts) == 0 {
		if ctx.BlockHeight() > s.store.LatestVersion() {
			s.store.SetLatestVersion(ctx.BlockHeight())
		}
		return nil
	}

	blockHash := common.Hash{}

	inputs := make([]parquet.ReceiptInput, 0, len(receipts))

	var (
		currentBlock  uint64
		logStartIndex uint
		maxBlock      uint64
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

		if currentBlock == 0 {
			currentBlock = blockNumber
		}
		if blockNumber != currentBlock {
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

		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquet.ReceiptInput{
			BlockNumber: blockNumber,
			Receipt: parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			},
			Logs:         BuildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
		})
	}

	if err := s.store.WriteReceipts(inputs); err != nil {
		return err
	}

	if s.txHashIndex != nil {
		if err := s.indexReceiptInputs(inputs); err != nil {
			return fmt.Errorf("tx hash index write failed: %w", err)
		}
	}

	if maxBlock > 0 {
		s.store.UpdateLatestVersion(int64(maxBlock)) //nolint:gosec // block numbers won't exceed int64 max
	}

	return nil
}

// indexReceiptInputs batches tx hashes by block number and writes them to the
// tx hash index.
func (s *parquetReceiptStore) indexReceiptInputs(inputs []parquet.ReceiptInput) error {
	type blockBatch struct {
		blockNumber uint64
		hashes      []common.Hash
	}
	var batches []blockBatch
	batchIdx := make(map[uint64]int)

	for i := range inputs {
		bn := inputs[i].BlockNumber
		txHash := common.BytesToHash(inputs[i].Receipt.TxHash)
		if idx, ok := batchIdx[bn]; ok {
			batches[idx].hashes = append(batches[idx].hashes, txHash)
		} else {
			batchIdx[bn] = len(batches)
			batches = append(batches, blockBatch{
				blockNumber: bn,
				hashes:      []common.Hash{txHash},
			})
		}
	}

	ctx := context.Background()
	for _, b := range batches {
		if err := s.txHashIndex.IndexBlock(ctx, b.blockNumber, b.hashes); err != nil {
			return err
		}
	}
	return nil
}

func (s *parquetReceiptStore) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}

	filter := parquet.LogFilter{
		FromBlock: &fromBlock,
		ToBlock:   &toBlock,
		Addresses: crit.Addresses,
		Topics:    crit.Topics,
	}

	results, err := s.store.GetLogs(ctx.Context(), filter)
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
		logEntry.Topics = buildTopicsFromParquetLogResult(lr)
		logs = append(logs, logEntry)
	}

	return logs, nil
}

func (s *parquetReceiptStore) Close() error {
	if s.indexPruner != nil {
		s.indexPruner.Stop()
	}
	storeErr := s.store.Close()
	if s.txHashIndex != nil {
		if err := s.txHashIndex.Close(); err != nil && storeErr == nil {
			storeErr = err
		}
	}
	return storeErr
}

func (s *parquetReceiptStore) replayWAL() error {
	wal := s.store.WAL()
	if wal == nil {
		return nil
	}

	firstOffset, errFirst := wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}

	var (
		currentBlock  uint64
		logStartIndex uint
		maxBlock      uint64
		dropOffset    uint64
	)

	// Collect tx hashes per block during replay so the index can be
	// populated in a single batch after the parquet store is consistent.
	type replayedBlock struct {
		blockNumber uint64
		hashes      []common.Hash
	}
	var replayedBlocks []replayedBlock
	replayIdx := make(map[uint64]int)

	blockHash := common.Hash{}
	fileStartBlock := s.store.FileStartBlock()

	err := wal.Replay(firstOffset, lastOffset, func(offset uint64, entry parquet.WALEntry) error {
		if len(entry.Receipts) == 0 {
			return nil
		}

		blockNumber := entry.BlockNumber
		if blockNumber < fileStartBlock {
			dropOffset = offset
			return nil
		}

		if currentBlock == 0 {
			currentBlock = blockNumber
		}
		if blockNumber != currentBlock {
			currentBlock = blockNumber
			logStartIndex = 0
		}

		for _, receiptBytes := range entry.Receipts {
			if len(receiptBytes) == 0 {
				continue
			}

			receipt := &types.Receipt{}
			if err := receipt.Unmarshal(receiptBytes); err != nil {
				return err
			}

			txHash := common.HexToHash(receipt.TxHashHex)
			s.store.WarmupRecords = append(s.store.WarmupRecords, parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(txHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			})

			if s.txHashIndex != nil {
				if idx, ok := replayIdx[blockNumber]; ok {
					replayedBlocks[idx].hashes = append(replayedBlocks[idx].hashes, txHash)
				} else {
					replayIdx[blockNumber] = len(replayedBlocks)
					replayedBlocks = append(replayedBlocks, replayedBlock{
						blockNumber: blockNumber,
						hashes:      []common.Hash{txHash},
					})
				}
			}

			txLogs := getLogsForTx(receipt, logStartIndex)
			logStartIndex += uint(len(txLogs))
			for _, lg := range txLogs {
				lg.BlockHash = blockHash
			}

			input := parquet.ReceiptInput{
				BlockNumber: blockNumber,
				Receipt: parquet.ReceiptRecord{
					TxHash:       parquet.CopyBytes(txHash[:]),
					BlockNumber:  blockNumber,
					ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
				},
				Logs: BuildParquetLogRecords(txLogs, blockHash),
			}

			if err := s.store.ApplyReceiptFromReplay(input); err != nil {
				return err
			}

			if blockNumber > maxBlock {
				maxBlock = blockNumber
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Re-index replayed blocks so the tx hash index stays consistent
	// with the parquet store after a crash/restart.
	if s.txHashIndex != nil {
		ctx := context.Background()
		for _, rb := range replayedBlocks {
			if err := s.txHashIndex.IndexBlock(ctx, rb.blockNumber, rb.hashes); err != nil {
				return fmt.Errorf("failed to re-index replayed block %d: %w", rb.blockNumber, err)
			}
		}
	}

	if maxBlock > 0 {
		s.store.UpdateLatestVersion(int64(maxBlock)) //nolint:gosec // block numbers won't exceed int64 max
	}
	if err := truncateReplayWAL(wal, dropOffset); err != nil {
		return err
	}
	return nil
}

func truncateReplayWAL(w interface{ TruncateBefore(offset uint64) error }, dropOffset uint64) error {
	if dropOffset == 0 {
		return nil
	}
	if err := w.TruncateBefore(dropOffset + 1); err != nil {
		if strings.Contains(err.Error(), "out of range") {
			return nil
		}
		return fmt.Errorf("failed to truncate replay WAL before offset %d: %w", dropOffset+1, err)
	}
	return nil
}

func BuildParquetLogRecords(logs []*ethtypes.Log, blockHash common.Hash) []parquet.LogRecord {
	if len(logs) == 0 {
		return nil
	}

	records := make([]parquet.LogRecord, 0, len(logs))
	for _, lg := range logs {
		topic0, topic1, topic2, topic3 := ExtractLogTopics(lg.Topics)
		rec := parquet.LogRecord{
			BlockNumber: lg.BlockNumber,
			TxHash:      lg.TxHash[:],
			TxIndex:     parquet.Uint32FromUint(lg.TxIndex),
			LogIndex:    parquet.Uint32FromUint(lg.Index),
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

func buildTopicsFromParquetLogResult(lr parquet.LogResult) []common.Hash {
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

func ExtractLogTopics(topics []common.Hash) ([]byte, []byte, []byte, []byte) {
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
