// Package parquet_v2 (receipt) is a V2 of the parquet-backed ReceiptStore.
//
// Compared to receipt.parquetReceiptStore, this implementation routes all
// operations through the V2 parquet store's coordinator goroutine. The store
// itself owns no extra mutexes; this wrapper only handles policy concerns
// (tx hash indexing, WAL replay, error normalization) on top of the
// coordinator-based parquet_v2.Store.
//
// The first pass intentionally does not integrate with receipt's in-memory
// cache (cached_receipt_store) because the cache integration interfaces in
// the receipt package are package-private. Wiring V2 into the cache layer is
// tracked as future work — it does not affect correctness of this PR.
package parquet_v2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	pq "github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet_v2"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "ledger-db", "receipt", "parquet-v2")

// ErrNotFound and ErrTxIndexDisabled are re-exported from the receipt package
// so callers depending on V2 don't need a transitive import.
var (
	ErrNotFound        = receipt.ErrNotFound
	ErrTxIndexDisabled = receipt.ErrTxIndexDisabled
)

// Store implements receipt.ReceiptStore on top of parquet_v2.Store.
type Store struct {
	store       *pq.Store
	storeKey    sdk.StoreKey
	txHashIndex receipt.TxHashIndex
	indexPruner *txHashIndexPruner
}

// Compile-time guarantee that Store satisfies receipt.ReceiptStore.
var _ receipt.ReceiptStore = (*Store)(nil)

// NewStore creates a V2 parquet receipt store. It mirrors the v1 constructor
// (newParquetReceiptStore) but delegates the underlying parquet I/O to the
// coordinator-based parquet_v2.Store.
func NewStore(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (receipt.ReceiptStore, error) {
	if cfg.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}

	txIndexBackend := dbconfig.NormalizeReceiptTxIndexBackend(cfg.TxIndexBackend)
	parquetTxIndexBackend := txIndexBackend
	if parquetTxIndexBackend == dbconfig.ReceiptTxIndexBackendNone {
		parquetTxIndexBackend = "none"
	}

	storeCfg := pq.StoreConfig{
		DBDirectory:          cfg.DBDirectory,
		KeepRecent:           int64(cfg.KeepRecent),
		PruneIntervalSeconds: int64(cfg.PruneIntervalSeconds),
		TxIndexBackend:       parquetTxIndexBackend,
	}

	store, err := pq.NewStore(storeCfg)
	if err != nil {
		return nil, err
	}

	wrapper := &Store{
		store:    store,
		storeKey: storeKey,
	}

	switch txIndexBackend {
	case dbconfig.ReceiptTxIndexBackendNone:
		// no-op
	case dbconfig.ReceiptTxIndexBackendPebble:
		idx, err := receipt.NewPebbleTxHashIndex(receipt.TxHashIndexDir(cfg.DBDirectory))
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

func (s *Store) LatestVersion() int64 {
	return s.store.LatestVersion()
}

func (s *Store) SetLatestVersion(version int64) error {
	s.store.SetLatestVersion(version)
	return nil
}

func (s *Store) SetEarliestVersion(version int64) error {
	s.store.SetEarliestVersion(version)
	return nil
}

func (s *Store) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.indexedReceiptLookup(ctx.Context(), txHash)
	if err != nil {
		return nil, err
	}
	if result != nil {
		r := &types.Receipt{}
		if err := r.Unmarshal(result.ReceiptBytes); err != nil {
			return nil, err
		}
		return r, nil
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

func (s *Store) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.indexedReceiptLookup(ctx.Context(), txHash)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrNotFound
	}
	r := &types.Receipt{}
	if err := r.Unmarshal(result.ReceiptBytes); err != nil {
		return nil, err
	}
	return r, nil
}

// indexedReceiptLookup uses the tx hash index to narrow the parquet search to
// a single file. When the index is disabled the lookup returns
// ErrTxIndexDisabled rather than fall back to a full scan.
func (s *Store) indexedReceiptLookup(ctx context.Context, txHash common.Hash) (*pq.ReceiptResult, error) {
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

func (s *Store) SetReceipts(ctx sdk.Context, receipts []receipt.ReceiptRecord) error {
	if len(receipts) == 0 {
		if ctx.BlockHeight() > 0 {
			if err := s.store.ObserveEmptyBlock(uint64(ctx.BlockHeight())); err != nil { //nolint:gosec // block heights fit within uint64
				return err
			}
		}
		if ctx.BlockHeight() > s.store.LatestVersion() {
			s.store.SetLatestVersion(ctx.BlockHeight())
		}
		return nil
	}

	blockHash := common.Hash{}

	inputs := make([]pq.ReceiptInput, 0, len(receipts))

	var (
		currentBlock  uint64
		logStartIndex uint
		maxBlock      uint64
	)

	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		r := record.Receipt
		blockNumber := r.BlockNumber
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
			receiptBytes, err = r.Marshal()
			if err != nil {
				return err
			}
		}

		txLogs := getLogsForTx(r, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, pq.ReceiptInput{
			BlockNumber: blockNumber,
			Receipt: pq.ReceiptRecord{
				TxHash:       pq.CopyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: pq.CopyBytesOrEmpty(receiptBytes),
			},
			Logs:         buildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: pq.CopyBytesOrEmpty(receiptBytes),
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

func (s *Store) indexReceiptInputs(inputs []pq.ReceiptInput) error {
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

func (s *Store) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}

	filter := pq.LogFilter{
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
		entry := &ethtypes.Log{
			BlockNumber: lr.BlockNumber,
			TxHash:      common.BytesToHash(lr.TxHash),
			TxIndex:     uint(lr.TxIndex),
			Index:       uint(lr.LogIndex),
			Data:        lr.Data,
			Removed:     lr.Removed,
			BlockHash:   common.BytesToHash(lr.BlockHash),
		}
		copy(entry.Address[:], lr.Address)
		entry.Topics = buildTopicsFromParquetLogResult(lr)
		logs = append(logs, entry)
	}

	return logs, nil
}

func (s *Store) Close() error {
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

// Underlying returns the V2 parquet store. Tests use this to drive fault
// injection / crash simulation; production code should not need it.
func (s *Store) Underlying() *pq.Store {
	return s.store
}

// SimulateCrash mimics process termination: it closes the tx hash index
// (releasing its Pebble lock) and crashes the underlying parquet store. After
// SimulateCrash returns, the directory may be reopened by a fresh NewStore.
//
// Test-only. A real process crash releases all OS resources automatically.
func (s *Store) SimulateCrash() {
	if s.indexPruner != nil {
		s.indexPruner.Stop()
		s.indexPruner = nil
	}
	if s.txHashIndex != nil {
		_ = s.txHashIndex.Close()
		s.txHashIndex = nil
	}
	s.store.SimulateCrash()
}

// replayWAL recovers state from the parquet WAL on startup. Mirrors v1
// parquetReceiptStore.replayWAL — semantics are preserved so crash recovery
// behavior matches.
func (s *Store) replayWAL() error {
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

	type replayedBlock struct {
		blockNumber uint64
		hashes      []common.Hash
	}
	var replayedBlocks []replayedBlock
	replayIdx := make(map[uint64]int)

	blockHash := common.Hash{}

	err := wal.Replay(firstOffset, lastOffset, func(offset uint64, entry pq.WALEntry) error {
		if len(entry.Receipts) == 0 {
			return nil
		}

		blockNumber := entry.BlockNumber
		if blockNumber < s.store.FileStartBlock() {
			dropOffset = offset
			return nil
		}

		// A boundary entry about to rotate makes every prior entry stale.
		// Advance dropOffset so the post-replay truncate removes them.
		if blockNumber != currentBlock && s.store.IsRotationBoundary(blockNumber) && blockNumber > s.store.FileStartBlock() {
			if offset > 0 {
				dropOffset = offset - 1
			}
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

			r := &types.Receipt{}
			if err := r.Unmarshal(receiptBytes); err != nil {
				return err
			}

			txHash := common.HexToHash(r.TxHashHex)

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

			txLogs := getLogsForTx(r, logStartIndex)
			logStartIndex += uint(len(txLogs))
			for _, lg := range txLogs {
				lg.BlockHash = blockHash
			}

			input := pq.ReceiptInput{
				BlockNumber: blockNumber,
				Receipt: pq.ReceiptRecord{
					TxHash:       pq.CopyBytes(txHash[:]),
					BlockNumber:  blockNumber,
					ReceiptBytes: pq.CopyBytesOrEmpty(receiptBytes),
				},
				Logs: buildParquetLogRecords(txLogs, blockHash),
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

func getLogsForTx(r *types.Receipt, logStartIndex uint) []*ethtypes.Log {
	return utils.Map(r.Logs, func(l *types.Log) *ethtypes.Log { return convertLog(l, r, logStartIndex) })
}

func convertLog(l *types.Log, r *types.Receipt, logStartIndex uint) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      utils.Map(l.Topics, common.HexToHash),
		Data:        l.Data,
		BlockNumber: r.BlockNumber,
		TxHash:      common.HexToHash(r.TxHashHex),
		TxIndex:     uint(r.TransactionIndex),
		Index:       uint(l.Index) + logStartIndex,
	}
}

func buildParquetLogRecords(logs []*ethtypes.Log, blockHash common.Hash) []pq.LogRecord {
	if len(logs) == 0 {
		return nil
	}

	records := make([]pq.LogRecord, 0, len(logs))
	for _, lg := range logs {
		topic0, topic1, topic2, topic3 := extractLogTopics(lg.Topics)
		rec := pq.LogRecord{
			BlockNumber: lg.BlockNumber,
			TxHash:      lg.TxHash[:],
			TxIndex:     pq.Uint32FromUint(lg.TxIndex),
			LogIndex:    pq.Uint32FromUint(lg.Index),
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

func buildTopicsFromParquetLogResult(lr pq.LogResult) []common.Hash {
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
