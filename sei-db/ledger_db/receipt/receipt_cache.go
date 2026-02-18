package receipt

import (
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const numCacheChunks = 3 // current (write+read), previous (read), oldest (prune)

type receiptCacheEntry struct {
	TxHash  common.Hash
	Receipt *types.Receipt
}

type receiptChunk struct {
	receipts     map[uint64]map[common.Hash]*types.Receipt // blockNum -> (txHash -> receipt)
	receiptIndex map[common.Hash]uint64                    // txHash -> blockNum
}

// ledgerCache stores recent receipts in rotating chunks.
// It keeps two most-recent chunks and prunes the oldest on rotation.
type ledgerCache struct {
	receiptChunks    [numCacheChunks]atomic.Pointer[receiptChunk]
	receiptWriteSlot atomic.Int32
	receiptMu        sync.RWMutex
}

func newLedgerCache() *ledgerCache {
	c := &ledgerCache{}

	firstReceiptChunk := &receiptChunk{
		receipts:     make(map[uint64]map[common.Hash]*types.Receipt),
		receiptIndex: make(map[common.Hash]uint64),
	}
	c.receiptChunks[0].Store(firstReceiptChunk)
	c.receiptWriteSlot.Store(0)

	return c
}

func (c *ledgerCache) Rotate() {
	// Rotate receipts
	c.receiptMu.Lock()
	oldReceiptSlot := c.receiptWriteSlot.Load()
	newReceiptSlot := (oldReceiptSlot + 1) % numCacheChunks
	pruneReceiptSlot := (newReceiptSlot + 1) % numCacheChunks

	newReceiptChunk := &receiptChunk{
		receipts:     make(map[uint64]map[common.Hash]*types.Receipt),
		receiptIndex: make(map[common.Hash]uint64),
	}
	c.receiptChunks[newReceiptSlot].Store(newReceiptChunk)
	c.receiptWriteSlot.Store(newReceiptSlot)
	c.receiptChunks[pruneReceiptSlot].Store(nil)
	c.receiptMu.Unlock()
}

func (c *ledgerCache) GetReceipt(txHash common.Hash) (*types.Receipt, bool) {
	c.receiptMu.RLock()
	defer c.receiptMu.RUnlock()

	writeSlot := c.receiptWriteSlot.Load()
	for i := int32(0); i < numCacheChunks; i++ {
		slot := (writeSlot - i + numCacheChunks) % numCacheChunks
		chunk := c.receiptChunks[slot].Load()
		if chunk == nil {
			continue
		}
		blockNum, exists := chunk.receiptIndex[txHash]
		if !exists {
			continue
		}
		blockReceipts, exists := chunk.receipts[blockNum]
		if !exists {
			continue
		}
		receipt, found := blockReceipts[txHash]
		if found {
			return receipt, true
		}
	}
	return nil, false
}

func (c *ledgerCache) AddReceiptsBatch(blockNumber uint64, receipts []receiptCacheEntry) {
	if len(receipts) == 0 {
		return
	}

	c.receiptMu.Lock()
	defer c.receiptMu.Unlock()

	slot := c.receiptWriteSlot.Load()
	chunk := c.receiptChunks[slot].Load()
	if chunk == nil {
		chunk = &receiptChunk{
			receipts:     make(map[uint64]map[common.Hash]*types.Receipt),
			receiptIndex: make(map[common.Hash]uint64),
		}
		c.receiptChunks[slot].Store(chunk)
	}

	if chunk.receipts[blockNumber] == nil {
		chunk.receipts[blockNumber] = make(map[common.Hash]*types.Receipt, len(receipts))
	}

	for i := range receipts {
		chunk.receipts[blockNumber][receipts[i].TxHash] = receipts[i].Receipt
		chunk.receiptIndex[receipts[i].TxHash] = blockNumber
	}
}
