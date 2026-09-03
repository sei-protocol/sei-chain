package evmonly

import (
	"fmt"
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

// MemoryReceiptStore retains receipts in memory by transaction hash and block.
type MemoryReceiptStore struct {
	mu sync.RWMutex

	latestVersion   int64
	earliestVersion int64
	blocks          map[uint64]map[common.Hash]*evmtypes.Receipt
	byTxHash        map[common.Hash]memoryReceiptEntry
}

// NewMemoryReceiptStore constructs an empty in-memory receipt store.
func NewMemoryReceiptStore() *MemoryReceiptStore {
	return &MemoryReceiptStore{
		blocks:   make(map[uint64]map[common.Hash]*evmtypes.Receipt),
		byTxHash: make(map[common.Hash]memoryReceiptEntry),
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
	}
	if err := receiptContextError(ctx); err != nil {
		return err
	}

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

// FilterLogs reports that the in-memory backend does not support range queries.
func (*MemoryReceiptStore) FilterLogs(
	ctx sdk.Context,
	_, _ uint64,
	_ filters.FilterCriteria,
	_ *receipt.LogBudget,
) ([]*ethtypes.Log, error) {
	if err := receiptContextError(ctx); err != nil {
		return nil, err
	}
	return nil, receipt.ErrRangeQueryNotSupported
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
