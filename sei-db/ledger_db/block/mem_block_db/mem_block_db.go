package memblockdb

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

// Shared backing store, keyed by path in test builders to simulate restarts.
type memBlockDBData struct {
	mu             sync.RWMutex
	blocksByHash   map[string]block.Block
	blocksByHeight map[uint64]block.Block
	txByHash       map[string]block.Transaction
	// txResultByHash holds result bytes set by SetTransactionResults. Kept
	// separate from txByHash so writes (block) and result-attachment
	// (post-execution) stay independent — a Transaction read before its
	// block executes returns nil from Result() rather than blocking.
	txResultByHash map[string][]byte
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
			blocksByHash:   make(map[string]block.Block),
			blocksByHeight: make(map[uint64]block.Block),
			txByHash:       make(map[string]block.Transaction),
			txResultByHash: make(map[string][]byte),
		},
	}
}

func (m *memBlockDB) WriteBlock(_ context.Context, blk block.Block) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	height := blk.Height()
	d.blocksByHash[string(blk.Hash())] = blk
	d.blocksByHeight[height] = blk
	for _, tx := range blk.Transactions() {
		d.txByHash[string(tx.Hash())] = tx
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
	for i, tx := range txs {
		// Eager copy of the result bytes so callers can release the source
		// adapter (and the underlying *abci.ExecTxResult) immediately after
		// SetTransactionResults returns.
		d.txResultByHash[string(tx.Hash())] = results[i].Bytes()
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

// composedTx layers a separately-stored result on top of a stored Transaction.
// Hash/Bytes/Height/Index come from the original Transaction; Result reflects
// whether SetTransactionResults has run for the parent block.
type composedTx struct {
	block.Transaction
	result    []byte
	hasResult bool
}

func (c composedTx) Result() ([]byte, bool) { return c.result, c.hasResult }

func (m *memBlockDB) GetTransactionByHash(_ context.Context, hash []byte) (block.Transaction, bool, error) {
	d := m.data
	d.mu.RLock()
	defer d.mu.RUnlock()

	tx, ok := d.txByHash[string(hash)]
	if !ok {
		return nil, false, nil
	}
	result, hasResult := d.txResultByHash[string(hash)]
	return composedTx{Transaction: tx, result: result, hasResult: hasResult}, true, nil
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
		for _, tx := range blk.Transactions() {
			delete(d.txByHash, string(tx.Hash()))
			delete(d.txResultByHash, string(tx.Hash()))
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
