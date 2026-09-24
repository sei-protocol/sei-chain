package evmonly

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var _ receipt.ReceiptStore = (*MemoryReceiptStore)(nil)

type memoryReceiptEntry struct {
	blockNumber uint64
	receipt     *evmtypes.Receipt
}

// MemoryReceiptStore is a process-local receipt store for unit tests. Runtime
// and load-test code uses the configured receipt backend instead.
type MemoryReceiptStore struct {
	mu sync.RWMutex

	latestVersion   int64
	earliestVersion int64
	blocks          map[uint64]map[common.Hash]*evmtypes.Receipt
	byTxHash        map[common.Hash]memoryReceiptEntry
	blockStats      map[uint64]receipt.BlockStats
}

// NewMemoryReceiptStore returns an empty MemoryReceiptStore.
func NewMemoryReceiptStore() *MemoryReceiptStore {
	return &MemoryReceiptStore{
		blocks:     make(map[uint64]map[common.Hash]*evmtypes.Receipt),
		byTxHash:   make(map[common.Hash]memoryReceiptEntry),
		blockStats: make(map[uint64]receipt.BlockStats),
	}
}

// Name returns the store name used by storage lifecycle logs.
func (*MemoryReceiptStore) Name() string {
	return "ReceiptDB"
}

// LatestVersion returns the greatest block height recorded by the store.
func (s *MemoryReceiptStore) LatestVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestVersion
}

// EarliestVersion returns the current receipt retention floor.
func (s *MemoryReceiptStore) EarliestVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.earliestVersion
}

// SetLatestVersion advances the greatest block height recorded by the store.
func (s *MemoryReceiptStore) SetLatestVersion(version int64) error {
	if version < 0 {
		return fmt.Errorf("receipt version must not be negative: %d", version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if version > s.latestVersion {
		s.latestVersion = version
	}
	return nil
}

// SetEarliestVersion advances the receipt retention floor.
func (s *MemoryReceiptStore) SetEarliestVersion(version int64) error {
	if version < 0 {
		return fmt.Errorf("receipt version must not be negative: %d", version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if version > s.earliestVersion {
		s.earliestVersion = version
	}
	return nil
}

// GetReceipt returns a caller-owned copy of the receipt for txHash.
func (s *MemoryReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*evmtypes.Receipt, error) {
	return s.GetReceiptFromStore(ctx, txHash)
}

// GetReceiptFromStore returns a caller-owned copy of the receipt for txHash.
func (s *MemoryReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*evmtypes.Receipt, error) {
	if err := receiptContextError(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.byTxHash[txHash]
	if !ok {
		return nil, receipt.ErrNotFound
	}
	if s.earliestVersion > 0 && entry.blockNumber < uint64(s.earliestVersion) { //nolint:gosec // earliestVersion is positive.
		return nil, receipt.ErrNotFound
	}
	return cloneStoredReceipt(entry.receipt), nil
}

// SetReceipts stores caller-owned copies of receipt records.
func (s *MemoryReceiptStore) SetReceipts(ctx sdk.Context, records []receipt.ReceiptRecord) error {
	if err := receiptContextError(ctx); err != nil {
		return err
	}
	if ctx.BlockHeight() < 0 {
		return fmt.Errorf("receipt block height must not be negative: %d", ctx.BlockHeight())
	}

	stored := make([]receipt.ReceiptRecord, 0, len(records))
	byBlock := make(map[uint64][]receipt.ReceiptRecord)
	latestVersion := ctx.BlockHeight()
	for _, record := range records {
		if record.Receipt == nil {
			continue
		}
		if record.Receipt.BlockNumber > maxGigaStoreBlockNumber {
			return fmt.Errorf("receipt block number %d exceeds int64", record.Receipt.BlockNumber)
		}
		if blockVersion := int64(record.Receipt.BlockNumber); blockVersion > latestVersion { //nolint:gosec // bounded above.
			latestVersion = blockVersion
		}
		stored = append(stored, receipt.ReceiptRecord{
			TxHash:  record.TxHash,
			Receipt: cloneStoredReceipt(record.Receipt),
		})
		byBlock[record.Receipt.BlockNumber] = append(byBlock[record.Receipt.BlockNumber], record)
	}
	if err := receiptContextError(ctx); err != nil {
		return err
	}
	if err := s.storeRecords(ctx, stored, latestVersion); err != nil {
		return err
	}
	s.mu.Lock()
	for blockNumber, blockRecords := range byBlock {
		s.blockStats[blockNumber] = receipt.ComputeBlockStats(blockRecords, receipt.DefaultRewardPercentiles)
	}
	if len(byBlock) == 0 && ctx.BlockHeight() > 0 {
		// An empty block still executed; record it as a real, zero-stat block rather than leaving
		// it unrecorded, which GetBlockStats would otherwise report as ErrBlockStatsNotSupported.
		blockNumber := uint64(ctx.BlockHeight()) //nolint:gosec // guarded non-negative above
		if _, exists := s.blockStats[blockNumber]; !exists {
			s.blockStats[blockNumber] = receipt.BlockStats{}
		}
	}
	s.mu.Unlock()
	receipt.RecordReceiptsWritten(ctx.Context(), stored)
	return nil
}

// GetBlockStats returns the aggregate stats recorded for blockNumber when its receipts were set.
func (s *MemoryReceiptStore) GetBlockStats(_ sdk.Context, blockNumber uint64) (receipt.BlockStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.earliestVersion > 0 && blockNumber < uint64(s.earliestVersion) { //nolint:gosec // earliestVersion is positive.
		return receipt.BlockStats{}, receipt.ErrNotFound
	}
	stats, ok := s.blockStats[blockNumber]
	if !ok {
		return receipt.BlockStats{}, receipt.ErrBlockStatsNotSupported
	}
	return stats, nil
}

// storeRecords installs a block's receipt records and advances the store version.
func (s *MemoryReceiptStore) storeRecords(ctx sdk.Context, stored []receipt.ReceiptRecord, latestVersion int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := receiptContextError(ctx); err != nil {
		return err
	}
	for _, record := range stored {
		if previous, ok := s.byTxHash[record.TxHash]; ok {
			delete(s.blocks[previous.blockNumber], record.TxHash)
			if len(s.blocks[previous.blockNumber]) == 0 {
				delete(s.blocks, previous.blockNumber)
				// A block a moved receipt vacates entirely gets a real, zero-stat entry.
				s.blockStats[previous.blockNumber] = receipt.BlockStats{}
			} else {
				// A block that still has other receipts after one moves away has its cached
				// stats invalidated, not recomputed.
				delete(s.blockStats, previous.blockNumber)
			}
		}
		blockNumber := record.Receipt.BlockNumber
		if s.blocks[blockNumber] == nil {
			s.blocks[blockNumber] = make(map[common.Hash]*evmtypes.Receipt)
		}
		s.blocks[blockNumber][record.TxHash] = record.Receipt
		s.byTxHash[record.TxHash] = memoryReceiptEntry{
			blockNumber: blockNumber,
			receipt:     record.Receipt,
		}
	}
	if latestVersion > s.latestVersion {
		s.latestVersion = latestVersion
	}
	return nil
}

// FilterLogs returns the logs in [fromBlock, toBlock] matching crit, in block
// then transaction order, with the same field conventions as the disk-backed
// stores: BlockHash is zero and Index carries the block-wide first-log offset
// of its transaction on top of the stored index.
func (s *MemoryReceiptStore) FilterLogs(
	ctx sdk.Context,
	fromBlock, toBlock uint64,
	crit filters.FilterCriteria,
	budget *receipt.LogBudget,
) ([]*ethtypes.Log, error) {
	if err := receiptContextError(ctx); err != nil {
		return nil, err
	}
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}
	it, err := s.IterateReceipts(fromBlock)
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()

	var logs []*ethtypes.Log
	var currentBlock uint64
	firstLogIndex := uint(0)
	for {
		ok, err := it.Next()
		if err != nil {
			return nil, err
		}
		if !ok || it.BlockNumber() > toBlock {
			return logs, nil
		}
		if it.BlockNumber() != currentBlock {
			currentBlock = it.BlockNumber()
			firstLogIndex = 0
		}
		stored, err := it.Receipt()
		if err != nil {
			return nil, err
		}
		for _, storedLog := range stored.Logs {
			if !storedLogMatches(storedLog, crit) {
				continue
			}
			lg := &ethtypes.Log{
				Address:     common.HexToAddress(storedLog.Address),
				Topics:      make([]common.Hash, len(storedLog.Topics)),
				Data:        append([]byte(nil), storedLog.Data...),
				BlockNumber: stored.BlockNumber,
				TxHash:      common.HexToHash(stored.TxHashHex),
				TxIndex:     uint(stored.TransactionIndex),
				Index:       uint(storedLog.Index) + firstLogIndex,
			}
			for i, topic := range storedLog.Topics {
				lg.Topics[i] = common.HexToHash(topic)
			}
			if err := budget.Reserve(lg); err != nil {
				return nil, err
			}
			logs = append(logs, lg)
		}
		firstLogIndex += uint(len(stored.Logs))
	}
}

// storedLogMatches applies crit to a stored log without materializing it.
func storedLogMatches(lg *evmtypes.Log, crit filters.FilterCriteria) bool {
	if len(crit.Addresses) > 0 && !slices.Contains(crit.Addresses, common.HexToAddress(lg.Address)) {
		return false
	}
	for i, topics := range crit.Topics {
		if len(topics) == 0 {
			continue
		}
		// Stored topics are hex strings while crit holds common.Hash, so each
		// comparison decodes a topic. Watch this for performance regressions;
		// a criteria type keyed on the stored representation would avoid it.
		if i >= len(lg.Topics) || !slices.Contains(topics, common.HexToHash(lg.Topics[i])) {
			return false
		}
	}
	return true
}

// IterateReceipts walks a snapshot of the retained receipts at or above
// startBlock in block then transaction order.
func (s *MemoryReceiptStore) IterateReceipts(startBlock uint64) (receipt.ReceiptIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.earliestVersion > 0 && startBlock < uint64(s.earliestVersion) { //nolint:gosec // earliestVersion is positive.
		startBlock = uint64(s.earliestVersion) //nolint:gosec // earliestVersion is positive.
	}
	var entries []memoryReceiptEntry
	for blockNumber, blockReceipts := range s.blocks {
		if blockNumber < startBlock {
			continue
		}
		for _, stored := range blockReceipts {
			entries = append(entries, memoryReceiptEntry{blockNumber: blockNumber, receipt: stored})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].blockNumber != entries[j].blockNumber {
			return entries[i].blockNumber < entries[j].blockNumber
		}
		return entries[i].receipt.TransactionIndex < entries[j].receipt.TransactionIndex
	})
	return &memoryReceiptIterator{entries: entries, pos: -1}, nil
}

// memoryReceiptIterator walks a sorted snapshot of MemoryReceiptStore entries.
type memoryReceiptIterator struct {
	entries []memoryReceiptEntry
	pos     int
}

// Next advances to the next receipt, reporting false once the walk is complete.
func (it *memoryReceiptIterator) Next() (bool, error) {
	if it.pos+1 >= len(it.entries) {
		it.pos = len(it.entries)
		return false, nil
	}
	it.pos++
	return true, nil
}

// BlockNumber returns the block holding the current receipt.
func (it *memoryReceiptIterator) BlockNumber() uint64 {
	return it.entries[it.pos].blockNumber
}

// TxHash returns the hash of the current receipt's transaction.
func (it *memoryReceiptIterator) TxHash() common.Hash {
	return common.HexToHash(it.entries[it.pos].receipt.TxHashHex)
}

// Receipt returns a caller-owned copy of the current receipt.
func (it *memoryReceiptIterator) Receipt() (*evmtypes.Receipt, error) {
	return cloneStoredReceipt(it.entries[it.pos].receipt), nil
}

// Close releases the iterator's snapshot.
func (it *memoryReceiptIterator) Close() error {
	it.entries = nil
	it.pos = 0
	return nil
}

// Close closes the receipt store.
func (*MemoryReceiptStore) Close() error {
	return nil
}

// ExternalPruning reports that retention is controlled by the shared collector.
func (*MemoryReceiptStore) ExternalPruning() bool {
	return true
}

// PruneHistory removes receipts strictly below blockNumber.
func (s *MemoryReceiptStore) PruneHistory(blockNumber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latestVersion <= 0 || blockNumber > uint64(s.latestVersion) { //nolint:gosec // latestVersion is positive.
		return nil
	}
	for height, blockReceipts := range s.blocks {
		if height >= blockNumber {
			continue
		}
		for txHash := range blockReceipts {
			delete(s.byTxHash, txHash)
		}
		delete(s.blocks, height)
		delete(s.blockStats, height)
	}
	// A stats entry can outlive its s.blocks entry (an empty block, or a receipt moved away by
	// storeRecords), so it needs its own pass rather than piggybacking on the loop above.
	for height := range s.blockStats {
		if height < blockNumber {
			delete(s.blockStats, height)
		}
	}
	if blockNumber <= maxGigaStoreBlockNumber && int64(blockNumber) > s.earliestVersion { //nolint:gosec // bounded above.
		s.earliestVersion = int64(blockNumber) //nolint:gosec // bounded above.
	}
	return nil
}

// PruneSnapshots is a no-op because receipts have no snapshots.
func (*MemoryReceiptStore) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns the earliest block a rollback may target.
func (s *MemoryReceiptStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the greatest block height recorded by the store.
func (s *MemoryReceiptStore) GetLatestBlock() (uint64, error) {
	latest := s.LatestVersion()
	if latest <= 0 {
		return 0, nil
	}
	return uint64(latest), nil //nolint:gosec // latest is positive.
}

func cloneStoredReceipt(stored *evmtypes.Receipt) *evmtypes.Receipt {
	if stored == nil {
		return nil
	}
	cloned := *stored
	cloned.LogsBloom = append([]byte(nil), stored.LogsBloom...)
	cloned.Logs = make([]*evmtypes.Log, len(stored.Logs))
	for i, log := range stored.Logs {
		if log == nil {
			continue
		}
		clonedLog := *log
		clonedLog.Topics = append([]string(nil), log.Topics...)
		clonedLog.Data = append([]byte(nil), log.Data...)
		cloned.Logs[i] = &clonedLog
	}
	return &cloned
}

func receiptContextError(ctx sdk.Context) error {
	if ctx.Context() == nil {
		return nil
	}
	return ctx.Context().Err()
}
