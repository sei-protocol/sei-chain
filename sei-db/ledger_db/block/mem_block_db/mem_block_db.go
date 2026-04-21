package memblockdb

import (
	"context"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

// Shared backing store, keyed by path in test builders to simulate restarts.
type memBlockDBData struct {
	mu             sync.RWMutex
	blocksByHash   map[string]*block.BinaryBlock
	blocksByHeight map[uint64]*block.BinaryBlock
	txByHash       map[string]*block.BinaryTransaction
	lowestHeight   uint64
	highestHeight  uint64
	hasBlocks      bool
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
			blocksByHash:   make(map[string]*block.BinaryBlock),
			blocksByHeight: make(map[uint64]*block.BinaryBlock),
			txByHash:       make(map[string]*block.BinaryTransaction),
		},
	}
}

func (m *memBlockDB) WriteBlock(_ context.Context, blk *block.BinaryBlock) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	d.blocksByHash[string(blk.Hash)] = blk
	d.blocksByHeight[blk.Height] = blk
	for _, tx := range blk.Transactions {
		d.txByHash[string(tx.Hash)] = tx
	}

	if !d.hasBlocks {
		d.lowestHeight = blk.Height
		d.highestHeight = blk.Height
		d.hasBlocks = true
	} else {
		if blk.Height < d.lowestHeight {
			d.lowestHeight = blk.Height
		}
		if blk.Height > d.highestHeight {
			d.highestHeight = blk.Height
		}
	}
	return nil
}

func (m *memBlockDB) Flush(_ context.Context) error {
	return nil
}

func (m *memBlockDB) GetBlockByHash(_ context.Context, hash []byte) (*block.BinaryBlock, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	blk, ok := d.blocksByHash[string(hash)]
	return blk, ok, nil
}

func (m *memBlockDB) GetBlockByHeight(_ context.Context, height uint64) (*block.BinaryBlock, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	blk, ok := d.blocksByHeight[height]
	return blk, ok, nil
}

func (m *memBlockDB) GetTransactionByHash(_ context.Context, hash []byte) (*block.BinaryTransaction, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	tx, ok := d.txByHash[string(hash)]
	return tx, ok, nil
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
		delete(d.blocksByHash, string(blk.Hash))
		for _, tx := range blk.Transactions {
			delete(d.txByHash, string(tx.Hash))
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
