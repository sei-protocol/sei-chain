package receipt

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type parquetReceiptStoreV2 struct {
	store       *parquet_v2.Store
	storeKey    sdk.StoreKey
	txHashIndex TxHashIndex
	indexPruner *txHashIndexPruner

	closeOnce sync.Once
	closeErr  error
}

func newParquetReceiptStoreV2(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	txIndexBackend := dbconfig.NormalizeReceiptTxIndexBackend(cfg.TxIndexBackend)
	parquetTxIndexBackend := txIndexBackend
	if parquetTxIndexBackend == dbconfig.ReceiptTxIndexBackendNone {
		parquetTxIndexBackend = "none"
	}

	store, err := parquet_v2.NewStore(parquet.StoreConfig{
		DBDirectory:          cfg.DBDirectory,
		KeepRecent:           int64(cfg.KeepRecent),
		PruneIntervalSeconds: int64(cfg.PruneIntervalSeconds),
		TxIndexBackend:       parquetTxIndexBackend,
		WALConverter:         replayConverterV2,
	})
	if err != nil {
		return nil, err
	}

	var txHashIndex TxHashIndex
	switch txIndexBackend {
	case dbconfig.ReceiptTxIndexBackendNone:
	case dbconfig.ReceiptTxIndexBackendPebble:
		idx, err := NewPebbleTxHashIndex(TxHashIndexDir(cfg.DBDirectory))
		if err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("failed to open tx hash index: %w", err)
		}
		txHashIndex = idx
	default:
		_ = store.Close()
		return nil, fmt.Errorf("unsupported receipt tx index backend: %s", txIndexBackend)
	}

	if txHashIndex != nil {
		ctx := context.Background()
		for _, rb := range store.ReplayedBlocks() {
			if err := txHashIndex.IndexBlock(ctx, rb.BlockNumber, rb.TxHashes); err != nil {
				_ = txHashIndex.Close()
				_ = store.Close()
				return nil, fmt.Errorf("failed to re-index replayed block %d: %w", rb.BlockNumber, err)
			}
		}
	}

	wrapper := &parquetReceiptStoreV2{
		store:       store,
		storeKey:    storeKey,
		txHashIndex: txHashIndex,
	}
	if txHashIndex != nil {
		wrapper.indexPruner = newTxHashIndexPruner(
			txHashIndex,
			int64(cfg.KeepRecent),
			int64(cfg.PruneIntervalSeconds),
			func() int64 { return store.LatestVersion() },
		)
		wrapper.indexPruner.Start()
	}

	return wrapper, nil
}

func replayConverterV2(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (parquet.ReplayReceipt, error) {
	receipt := &types.Receipt{}
	if err := receipt.Unmarshal(receiptBytes); err != nil {
		return parquet.ReplayReceipt{}, err
	}

	txHash := common.HexToHash(receipt.TxHashHex)
	blockHash := common.Hash{}
	txLogs := getLogsForTx(receipt, logStartIndex)
	for _, lg := range txLogs {
		lg.BlockHash = blockHash
	}

	record := parquet.ReceiptRecord{
		TxHash:       parquet.CopyBytes(txHash[:]),
		BlockNumber:  blockNumber,
		ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
	}
	return parquet.ReplayReceipt{
		Input: parquet.ReceiptInput{
			Receipt:      record,
			Logs:         BuildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
		},
		TxHash:   txHash,
		Warmup:   record,
		LogCount: uint(len(txLogs)),
	}, nil
}

func (s *parquetReceiptStoreV2) LatestVersion() int64 {
	return s.store.LatestVersion()
}

func (s *parquetReceiptStoreV2) SetLatestVersion(version int64) error {
	s.store.SetLatestVersion(version)
	return nil
}

func (s *parquetReceiptStoreV2) SetEarliestVersion(version int64) error {
	s.store.SetEarliestVersion(version)
	return nil
}

func (s *parquetReceiptStoreV2) cacheRotateInterval() uint64 {
	return s.store.CacheRotateInterval()
}

func (s *parquetReceiptStoreV2) warmupReceipts() []ReceiptRecord {
	raw := s.store.WarmupRecords()
	records := make([]ReceiptRecord, 0, len(raw))
	for _, rec := range raw {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(rec.ReceiptBytes); err != nil {
			continue
		}
		records = append(records, ReceiptRecord{
			TxHash:  common.BytesToHash(rec.TxHash),
			Receipt: receipt,
		})
	}
	return records
}

func (s *parquetReceiptStoreV2) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
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

func (s *parquetReceiptStoreV2) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
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

func (s *parquetReceiptStoreV2) indexedReceiptLookup(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
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

func (s *parquetReceiptStoreV2) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	height := uint64(ctx.BlockHeight()) //nolint:gosec // block heights fit within uint64

	inputs, err := buildParquetReceiptInputs(receipts)
	if err != nil {
		return err
	}

	var inputBatches []receiptInputBlockBatch
	if len(inputs) == 0 {
		if err := s.store.WriteReceipts(height, nil); err != nil {
			return err
		}
	} else {
		inputBatches = groupReceiptInputsByBlock(inputs)
		for _, batch := range inputBatches {
			if err := s.store.WriteReceipts(batch.blockNumber, batch.inputs); err != nil {
				return err
			}
		}
	}

	if s.txHashIndex != nil && len(inputBatches) > 0 {
		if err := s.indexReceiptInputBatches(inputBatches); err != nil {
			return fmt.Errorf("tx hash index write failed: %w", err)
		}
	}

	s.store.UpdateLatestVersion(ctx.BlockHeight())
	return nil
}

// buildParquetReceiptInputs constructs ReceiptInputs for the v2 store. The
// wrapper-level BlockNumber field is intentionally left zero — v2 carries the
// committed height as an explicit parameter to WriteReceipts. The
// Receipt.BlockNumber column is still populated since it is what gets written
// to the parquet file.
func buildParquetReceiptInputs(receipts []ReceiptRecord) ([]parquet.ReceiptInput, error) {
	blockHash := common.Hash{}
	inputs := make([]parquet.ReceiptInput, 0, len(receipts))

	var (
		currentBlock  uint64
		haveBlock     bool
		logStartIndex uint
	)

	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}

		receipt := record.Receipt
		blockNumber := receipt.BlockNumber

		if !haveBlock || blockNumber != currentBlock {
			currentBlock = blockNumber
			haveBlock = true
			logStartIndex = 0
		}

		receiptBytes := record.ReceiptBytes
		if len(receiptBytes) == 0 {
			var err error
			receiptBytes, err = receipt.Marshal()
			if err != nil {
				return nil, err
			}
		}

		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquet.ReceiptInput{
			Receipt: parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			},
			Logs:         BuildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
		})
	}

	return inputs, nil
}

type receiptInputBlockBatch struct {
	blockNumber uint64
	inputs      []parquet.ReceiptInput
}

func groupReceiptInputsByBlock(inputs []parquet.ReceiptInput) []receiptInputBlockBatch {
	batchIndexes := make(map[uint64]int)
	batches := make([]receiptInputBlockBatch, 0)
	for _, input := range inputs {
		blockNumber := input.Receipt.BlockNumber
		if idx, exists := batchIndexes[blockNumber]; exists {
			batches[idx].inputs = append(batches[idx].inputs, input)
			continue
		}
		batchIndexes[blockNumber] = len(batches)
		batches = append(batches, receiptInputBlockBatch{
			blockNumber: blockNumber,
			inputs:      []parquet.ReceiptInput{input},
		})
	}

	sort.Slice(batches, func(i, j int) bool {
		return batches[i].blockNumber < batches[j].blockNumber
	})
	return batches
}

func (s *parquetReceiptStoreV2) indexReceiptInputBatches(batches []receiptInputBlockBatch) error {
	ctx := context.Background()
	for _, batch := range batches {
		hashes := make([]common.Hash, len(batch.inputs))
		for i := range batch.inputs {
			hashes[i] = common.BytesToHash(batch.inputs[i].Receipt.TxHash)
		}
		if err := s.txHashIndex.IndexBlock(ctx, batch.blockNumber, hashes); err != nil {
			return err
		}
	}
	return nil
}

func (s *parquetReceiptStoreV2) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}

	results, err := s.store.GetLogs(ctx.Context(), parquet.LogFilter{
		FromBlock: &fromBlock,
		ToBlock:   &toBlock,
		Addresses: crit.Addresses,
		Topics:    crit.Topics,
	})
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

// Close releases the parquet store, the tx-hash index, and the index pruner.
// Idempotent: indexPruner.Stop closes a channel that would panic on a second
// call, so the entire teardown is gated on closeOnce. Repeat callers receive
// the same error as the first.
func (s *parquetReceiptStoreV2) Close() error {
	s.closeOnce.Do(func() {
		if s.indexPruner != nil {
			s.indexPruner.Stop()
		}
		s.closeErr = s.store.Close()
		if s.txHashIndex != nil {
			if err := s.txHashIndex.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}
