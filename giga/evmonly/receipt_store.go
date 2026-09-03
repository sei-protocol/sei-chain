package evmonly

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// ReceiptStore persists and retrieves Ethereum receipts by block and transaction hash.
type ReceiptStore interface {
	// SetReceipts replaces the receipts for blockNumber. It must not retain
	// references to receipts after returning.
	SetReceipts(ctx context.Context, blockNumber uint64, receipts ethtypes.Receipts) error
	// GetReceipt returns a caller-owned receipt and whether txHash was found.
	GetReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, bool, error)
	// GetBlockReceipts returns caller-owned receipts and whether blockNumber was found.
	GetBlockReceipts(ctx context.Context, blockNumber uint64) (ethtypes.Receipts, bool, error)
}

var _ ReceiptStore = (*MemoryReceiptStore)(nil)

type memoryReceiptEntry struct {
	blockNumber uint64
	receipt     *ethtypes.Receipt
}

// MemoryReceiptStore retains cloned receipts in memory.
type MemoryReceiptStore struct {
	mu       sync.RWMutex
	blocks   map[uint64]ethtypes.Receipts
	byTxHash map[common.Hash]memoryReceiptEntry
}

// NewMemoryReceiptStore constructs an empty in-memory receipt store.
func NewMemoryReceiptStore() *MemoryReceiptStore {
	return &MemoryReceiptStore{
		blocks:   make(map[uint64]ethtypes.Receipts),
		byTxHash: make(map[common.Hash]memoryReceiptEntry),
	}
}

// SetReceipts replaces the receipts stored for blockNumber.
func (s *MemoryReceiptStore) SetReceipts(ctx context.Context, blockNumber uint64, receipts ethtypes.Receipts) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stored := cloneReceipts(receipts)
	for i, receipt := range stored {
		if receipt == nil {
			return fmt.Errorf("receipt %d for block %d is nil", i, blockNumber)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, receipt := range s.blocks[blockNumber] {
		entry, ok := s.byTxHash[receipt.TxHash]
		if ok && entry.blockNumber == blockNumber {
			delete(s.byTxHash, receipt.TxHash)
		}
	}
	s.blocks[blockNumber] = stored
	for _, receipt := range stored {
		s.byTxHash[receipt.TxHash] = memoryReceiptEntry{blockNumber: blockNumber, receipt: receipt}
	}
	return nil
}

// GetReceipt returns the receipt for txHash.
func (s *MemoryReceiptStore) GetReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.byTxHash[txHash]
	if !ok {
		return nil, false, nil
	}
	return cloneReceipt(entry.receipt), true, nil
}

// GetBlockReceipts returns the receipts stored for blockNumber in transaction order.
func (s *MemoryReceiptStore) GetBlockReceipts(ctx context.Context, blockNumber uint64) (ethtypes.Receipts, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipts, ok := s.blocks[blockNumber]
	if !ok {
		return nil, false, nil
	}
	return cloneReceipts(receipts), true, nil
}

func cloneReceipts(receipts ethtypes.Receipts) ethtypes.Receipts {
	cloned := make(ethtypes.Receipts, len(receipts))
	for i, receipt := range receipts {
		cloned[i] = cloneReceipt(receipt)
	}
	return cloned
}

func cloneReceipt(receipt *ethtypes.Receipt) *ethtypes.Receipt {
	if receipt == nil {
		return nil
	}
	cloned := *receipt
	cloned.PostState = slices.Clone(receipt.PostState)
	cloned.EffectiveGasPrice = cloneOptionalBig(receipt.EffectiveGasPrice)
	cloned.BlobGasPrice = cloneOptionalBig(receipt.BlobGasPrice)
	cloned.BlockNumber = cloneOptionalBig(receipt.BlockNumber)
	cloned.Logs = slices.Clone(receipt.Logs)
	for i, log := range receipt.Logs {
		if log == nil {
			continue
		}
		clonedLog := *log
		clonedLog.Topics = slices.Clone(log.Topics)
		clonedLog.Data = slices.Clone(log.Data)
		cloned.Logs[i] = &clonedLog
	}
	return &cloned
}
