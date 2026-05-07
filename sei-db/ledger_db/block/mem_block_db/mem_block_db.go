package memblockdb

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

// resultInstance holds the per-block-occurrence data for one tx hash:
// where the tx landed (height + index in that block) and, once
// SetTransactionResults has run, the marshaled execution result. bytes is
// nil while the entry is "pending" (block written, results not yet attached);
// composedResult on read filters those out.
type resultInstance struct {
	height uint64
	index  uint32
	bytes  []byte // nil if no result attached yet
}

// txEntry holds the invariant tx body once per hash, plus a per-block-hash
// map of resultInstance recording every block this tx appeared in.
type txEntry struct {
	tx        block.Transaction
	instances map[string]*resultInstance // blockHash -> instance
}

// composedResult adapts a resultInstance into the block.Result interface for
// the read path. It's value-typed and held briefly per call so allocations
// stay cheap.
type composedResult struct {
	height uint64
	index  uint32
	bytes  []byte
}

func (r composedResult) Bytes() []byte  { return r.bytes }
func (r composedResult) Height() uint64 { return r.height }
func (r composedResult) Index() uint32  { return r.index }

// Shared backing store, keyed by path in test builders to simulate restarts.
type memBlockDBData struct {
	mu             sync.RWMutex
	blocksByHash   map[string]block.Block
	blocksByHeight map[uint64]block.Block
	// txEntries is the two-level index: tx hash -> per-block instances.
	// Same tx hash appearing in multiple blocks (different lanes producing
	// the same tx) gets one entry per block in the inner map; pruning a
	// single block only removes that block's instance and leaves siblings
	// intact.
	txEntries     map[string]*txEntry
	lowestHeight  uint64
	highestHeight uint64
	hasBlocks     bool
}

// An in-memory implementation of the BlockDB interface. Useful as a test fixture to sanity check
// test flows.
type memBlockDB struct {
	data *memBlockDBData
}

// NewMemBlockDB creates an in-memory BlockDB suitable for testing and benchmarks.
func NewMemBlockDB() block.BlockDB {
	return &memBlockDB{
		data: &memBlockDBData{
			blocksByHash:   make(map[string]block.Block),
			blocksByHeight: make(map[uint64]block.Block),
			txEntries:      make(map[string]*txEntry),
		},
	}
}

func (m *memBlockDB) WriteBlock(_ context.Context, blk block.Block) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	height := blk.Height()
	blockHashKey := string(blk.Hash())
	d.blocksByHash[blockHashKey] = blk
	d.blocksByHeight[height] = blk
	for i, tx := range blk.Transactions() {
		hashKey := string(tx.Hash())
		entry, ok := d.txEntries[hashKey]
		if !ok {
			// First time we've seen this tx — record the canonical body.
			entry = &txEntry{
				tx:        tx,
				instances: make(map[string]*resultInstance),
			}
			d.txEntries[hashKey] = entry
		}
		// Register a pending instance for this block, even if we've recorded
		// the same tx hash for another block already. SetTransactionResults
		// will fill in bytes later.
		entry.instances[blockHashKey] = &resultInstance{
			height: height,
			index:  uint32(i), //nolint:gosec // tx index fits in uint32 (block tx count is bounded).
		}
	}

	if !d.hasBlocks {
		d.lowestHeight = height
		d.highestHeight = height
		d.hasBlocks = true
	} else {
		if height < d.lowestHeight {
			d.lowestHeight = height
		}
		if height > d.highestHeight {
			d.highestHeight = height
		}
	}
	return nil
}

func (m *memBlockDB) SetTransactionResults(_ context.Context, blockHash []byte, results []block.Result) error {
	// Marshal happens via results[i].Bytes(). The Result interface contract
	// permits this to be cheap (typically a single proto Marshal of an
	// already-built message), so we call it inside the write lock without
	// pre-buffering.
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	blk, ok := d.blocksByHash[string(blockHash)]
	if !ok {
		return fmt.Errorf("%w: %x", block.ErrUnknownBlock, blockHash)
	}
	txs := blk.Transactions()
	if len(txs) != len(results) {
		return fmt.Errorf("%w: block has %d txs, got %d results", block.ErrResultCountMismatch, len(txs), len(results))
	}
	blockHashKey := string(blockHash)
	for i, tx := range txs {
		entry, ok := d.txEntries[string(tx.Hash())]
		if !ok {
			// Defensive: WriteBlock should have created this entry. If it
			// didn't, the index is corrupted — surface loudly.
			return fmt.Errorf("internal: tx index missing entry for tx %x in block %x", tx.Hash(), blockHash)
		}
		inst, ok := entry.instances[blockHashKey]
		if !ok {
			return fmt.Errorf("internal: tx index missing instance for tx %x in block %x", tx.Hash(), blockHash)
		}
		inst.bytes = results[i].Bytes()
	}
	return nil
}

func (m *memBlockDB) Flush(_ context.Context) error {
	return nil
}

func (m *memBlockDB) GetBlockByHash(_ context.Context, hash []byte) (block.Block, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	blk, ok := d.blocksByHash[string(hash)]
	return blk, ok, nil
}

func (m *memBlockDB) GetBlockByHeight(_ context.Context, height uint64) (block.Block, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	blk, ok := d.blocksByHeight[height]
	return blk, ok, nil
}

func (m *memBlockDB) GetTransactionByHash(_ context.Context, hash []byte) (block.Transaction, []block.Result, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, ok := d.txEntries[string(hash)]
	if !ok {
		return nil, nil, false, nil
	}
	// Build the slice with only attached-result instances; pending entries
	// (bytes==nil) are filtered out so callers get exactly the executions.
	// Pre-size to len(instances) — typically 1, occasionally 2-3.
	results := make([]block.Result, 0, len(entry.instances))
	for _, inst := range entry.instances {
		if inst.bytes == nil {
			continue
		}
		results = append(results, composedResult{
			height: inst.height,
			index:  inst.index,
			bytes:  inst.bytes,
		})
	}
	return entry.tx, results, true, nil
}

func (m *memBlockDB) Prune(_ context.Context, lowestHeightToKeep uint64) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.hasBlocks || lowestHeightToKeep <= d.lowestHeight {
		return nil
	}

	for h := d.lowestHeight; h < lowestHeightToKeep && h <= d.highestHeight; h++ {
		blk, ok := d.blocksByHeight[h]
		if !ok {
			continue
		}
		delete(d.blocksByHeight, h)
		blockHashKey := string(blk.Hash())
		delete(d.blocksByHash, blockHashKey)
		for _, tx := range blk.Transactions() {
			hashKey := string(tx.Hash())
			entry, ok := d.txEntries[hashKey]
			if !ok {
				continue
			}
			// Only remove the instance for the block being pruned; other
			// blocks containing the same tx hash stay reachable.
			delete(entry.instances, blockHashKey)
			if len(entry.instances) == 0 {
				delete(d.txEntries, hashKey)
			}
		}
	}

	if lowestHeightToKeep > d.highestHeight {
		d.hasBlocks = false
	} else {
		d.lowestHeight = lowestHeightToKeep
	}
	return nil
}

func (m *memBlockDB) GetLowestBlockHeight(_ context.Context) (uint64, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.hasBlocks {
		return 0, block.ErrNoBlocks
	}
	return d.lowestHeight, nil
}

func (m *memBlockDB) GetHighestBlockHeight(_ context.Context) (uint64, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.hasBlocks {
		return 0, block.ErrNoBlocks
	}
	return d.highestHeight, nil
}

func (m *memBlockDB) Close(_ context.Context) error {
	return nil
}
