package receipt

import (
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const numCacheChunks = 3 // current (write+read), previous (read), oldest (prune)

type receiptCacheEntry struct {
	TxHash  common.Hash
	Receipt *types.Receipt
}

type logChunk struct {
	logs map[uint64][]*ethtypes.Log // blockNum -> logs
}

type receiptChunk struct {
	receipts     map[uint64]map[common.Hash]*types.Receipt // blockNum -> (txHash -> receipt)
	receiptIndex map[common.Hash]uint64                    // txHash -> blockNum
}

// ledgerCache stores recent receipts and logs in rotating chunks.
// It keeps two most-recent chunks and prunes the oldest on rotation.
type ledgerCache struct {
	logChunks    [numCacheChunks]atomic.Pointer[logChunk]
	logWriteSlot atomic.Int32
	logMu        sync.RWMutex

	receiptChunks    [numCacheChunks]atomic.Pointer[receiptChunk]
	receiptWriteSlot atomic.Int32
	receiptMu        sync.RWMutex
}

func newLedgerCache() *ledgerCache {
	c := &ledgerCache{}

	firstLogChunk := &logChunk{
		logs: make(map[uint64][]*ethtypes.Log),
	}
	c.logChunks[0].Store(firstLogChunk)
	c.logWriteSlot.Store(0)

	firstReceiptChunk := &receiptChunk{
		receipts:     make(map[uint64]map[common.Hash]*types.Receipt),
		receiptIndex: make(map[common.Hash]uint64),
	}
	c.receiptChunks[0].Store(firstReceiptChunk)
	c.receiptWriteSlot.Store(0)

	return c
}

func (c *ledgerCache) Rotate() {
	// Rotate logs
	c.logMu.Lock()
	oldLogSlot := c.logWriteSlot.Load()
	newLogSlot := (oldLogSlot + 1) % numCacheChunks
	pruneLogSlot := (newLogSlot + 1) % numCacheChunks

	newLogChunk := &logChunk{
		logs: make(map[uint64][]*ethtypes.Log),
	}
	c.logChunks[newLogSlot].Store(newLogChunk)
	c.logWriteSlot.Store(newLogSlot)
	c.logChunks[pruneLogSlot].Store(nil)
	c.logMu.Unlock()

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
			// Callers (e.g. RPC response formatting) may normalize TransactionIndex in-place.
			// Clone to avoid mutating the cached receipt and corrupting future lookups.
			return cloneReceipt(receipt), true
		}
	}
	return nil, false
}

// cloneReceipt makes a deep copy to keep cached receipts immutable to callers.
func cloneReceipt(r *types.Receipt) *types.Receipt {
	if r == nil {
		return nil
	}
	c := *r
	if r.Logs != nil {
		logs := make([]*types.Log, len(r.Logs))
		for i, lg := range r.Logs {
			if lg == nil {
				continue
			}
			logCopy := *lg
			if lg.Topics != nil {
				logCopy.Topics = append([]string(nil), lg.Topics...)
			}
			if lg.Data != nil {
				logCopy.Data = append([]byte(nil), lg.Data...)
			}
			logs[i] = &logCopy
		}
		c.Logs = logs
	}
	if r.LogsBloom != nil {
		c.LogsBloom = append([]byte(nil), r.LogsBloom...)
	}
	return &c
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

func (c *ledgerCache) AddLogsForBlock(blockNumber uint64, logs []*ethtypes.Log) {
	if len(logs) == 0 {
		return
	}

	logsCopy := make([]*ethtypes.Log, len(logs))
	for i, lg := range logs {
		logCopy := *lg
		logsCopy[i] = &logCopy
	}

	c.logMu.Lock()
	defer c.logMu.Unlock()

	slot := c.logWriteSlot.Load()
	chunk := c.logChunks[slot].Load()
	if chunk == nil {
		chunk = &logChunk{
			logs: make(map[uint64][]*ethtypes.Log),
		}
		c.logChunks[slot].Store(chunk)
	}
	chunk.logs[blockNumber] = logsCopy
}

// FilterLogs returns cached logs matching the filter criteria.
func (c *ledgerCache) FilterLogs(fromBlock, toBlock uint64, crit filters.FilterCriteria) []*ethtypes.Log {
	c.logMu.RLock()
	defer c.logMu.RUnlock()

	var result []*ethtypes.Log
	for i := 0; i < numCacheChunks; i++ {
		chunk := c.logChunks[i].Load()
		if chunk == nil {
			continue
		}
		for blockNum, logs := range chunk.logs {
			if blockNum < fromBlock || blockNum > toBlock {
				continue
			}
			for _, lg := range logs {
				if matchLog(lg, crit) {
					logCopy := *lg
					result = append(result, &logCopy)
				}
			}
		}
	}
	return result
}

// matchLog checks if a log matches the filter criteria.
func matchLog(lg *ethtypes.Log, crit filters.FilterCriteria) bool {
	// Check address filter
	if len(crit.Addresses) > 0 {
		found := false
		for _, addr := range crit.Addresses {
			if lg.Address == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check topic filters
	for i, topicList := range crit.Topics {
		if len(topicList) == 0 {
			continue
		}
		if i >= len(lg.Topics) {
			return false
		}
		found := false
		for _, topic := range topicList {
			if lg.Topics[i] == topic {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
