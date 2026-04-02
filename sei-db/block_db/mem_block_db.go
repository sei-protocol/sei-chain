package blockdb

import (
	"context"
	"sync"
)

// Shared backing store, keyed by path in test builders to simulate restarts.
type memBlockDBData struct {
	mu             sync.RWMutex
	blocksByHash   map[string]*BinaryBlock
	blocksByHeight map[uint64]*BinaryBlock
	txByHash       map[string]*BinaryTransaction
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
func NewMemBlockDB() BlockDB {
	return &memBlockDB{
		data: &memBlockDBData{
			blocksByHash:   make(map[string]*BinaryBlock),
			blocksByHeight: make(map[uint64]*BinaryBlock),
			txByHash:       make(map[string]*BinaryTransaction),
		},
	}
}

func (m *memBlockDB) WriteBlock(_ context.Context, block *BinaryBlock) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	d.blocksByHash[string(block.Hash)] = block
	d.blocksByHeight[block.Height] = block
	for _, tx := range block.Transactions {
		d.txByHash[string(tx.Hash)] = tx
	}

	if !d.hasBlocks {
		d.lowestHeight = block.Height
		d.highestHeight = block.Height
		d.hasBlocks = true
	} else {
		if block.Height < d.lowestHeight {
			d.lowestHeight = block.Height
		}
		if block.Height > d.highestHeight {
			d.highestHeight = block.Height
		}
	}
	return nil
}

func (m *memBlockDB) Flush(_ context.Context) error {
	return nil
}

func (m *memBlockDB) GetBlockByHash(_ context.Context, hash []byte) (*BinaryBlock, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	block, ok := d.blocksByHash[string(hash)]
	return block, ok, nil
}

func (m *memBlockDB) GetBlockByHeight(_ context.Context, height uint64) (*BinaryBlock, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	block, ok := d.blocksByHeight[height]
	return block, ok, nil
}

func (m *memBlockDB) GetTransactionByHash(_ context.Context, hash []byte) (*BinaryTransaction, bool, error) {
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
		block, ok := d.blocksByHeight[h]
		if !ok {
			continue
		}
		delete(d.blocksByHeight, h)
		delete(d.blocksByHash, string(block.Hash))
		for _, tx := range block.Transactions {
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

func (m *memBlockDB) Close(_ context.Context) error {
	return nil
}
