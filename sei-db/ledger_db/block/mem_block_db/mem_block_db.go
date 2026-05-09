package memblockdb

import (
	"context"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

// Shared backing store, keyed by path in test builders to simulate restarts.
type memBlockDBData struct {
	mu             sync.RWMutex
	blocksByHash   map[string]block.Block
	blocksByHeight map[uint64]block.Block
	lowestHeight   uint64
	highestHeight  uint64
	hasBlocks      bool
}

// An in-memory implementation of the BlockDB interface. Useful as a test
// fixture and as a development-time backend until a persistent BlockDB lands.
//
// TODO(blockdb): add a -race concurrency test — every public method's lock
// shape (WriteBlock under write lock; Get* under read lock) is currently
// verified only by inspection.
type memBlockDB struct {
	data *memBlockDBData
}

// NewMemBlockDB creates an in-memory BlockDB suitable for testing and benchmarks.
func NewMemBlockDB() block.BlockDB {
	return &memBlockDB{
		data: &memBlockDBData{
			blocksByHash:   make(map[string]block.Block),
			blocksByHeight: make(map[uint64]block.Block),
		},
	}
}

func (m *memBlockDB) WriteBlock(_ context.Context, blk block.Block) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	blockHashKey := string(blk.Hash())
	// Idempotent on duplicate: treat a second WriteBlock for the same block
	// hash as a no-op rather than overwriting indexes.
	if _, exists := d.blocksByHash[blockHashKey]; exists {
		return nil
	}
	height := blk.Height()
	d.blocksByHash[blockHashKey] = blk
	d.blocksByHeight[height] = blk

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
		delete(d.blocksByHash, string(blk.Hash()))
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
