package memblockdb

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

// resultInstance is the per-block-occurrence record for one tx hash. It
// carries the location (height + position in the block) plus the marshaled
// execution result (nil while the entry is "pending" — block written but
// SetTransactionResults not yet called). The same struct value satisfies
// block.Result on read; GetTransactionByHash returns a value-copy so the
// caller's bytes slice header is independent of the storage's. A later
// SetTransactionResults reassigns inst.bytes (does not mutate it in
// place), so the caller's earlier read is naturally isolated by Go's
// slice-header-copy semantics — no defensive deep-copy needed here.
// (If a future caller wants to mutate the returned bytes in place, they
// must copy first.)
type resultInstance struct {
	height uint64
	index  uint32
	bytes  []byte // nil if no result attached yet
}

func (r resultInstance) Bytes() []byte  { return r.bytes }
func (r resultInstance) Height() uint64 { return r.height }
func (r resultInstance) Index() uint32  { return r.index }

// txEntry holds the invariant tx body once per hash, plus a per-block-hash
// map of resultInstance recording every block this tx appeared in.
type txEntry struct {
	tx        block.Transaction
	instances map[string]*resultInstance // blockHash -> instance
}

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
//
// TODO(blockdb): add a -race concurrency test — every public method's lock
// shape (WriteBlock + SetTransactionResults under write lock; Get* under
// read lock; two-pass validate-then-mutate in WriteBlock) is currently
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
			txEntries:      make(map[string]*txEntry),
		},
	}
}

func (m *memBlockDB) WriteBlock(_ context.Context, blk block.Block) error {
	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	blockHashKey := string(blk.Hash())
	// Idempotent on duplicate: a second WriteBlock for the same block hash
	// would re-create resultInstance entries with bytes=nil, silently
	// destroying anything SetTransactionResults already attached. Skip.
	if _, exists := d.blocksByHash[blockHashKey]; exists {
		return nil
	}
	height := blk.Height()
	txs := blk.Transactions()

	// First pass: validate every tx body against any pre-existing entry for
	// the same hash. A mismatch surfaces a tx-hash collision (two distinct
	// bodies hashing to the same value) — refuse the entire write rather
	// than partially mutate state.
	for _, tx := range txs {
		entry, ok := d.txEntries[string(tx.Hash())]
		if !ok {
			continue
		}
		if !bytes.Equal(entry.tx.Bytes(), tx.Bytes()) {
			return fmt.Errorf("%w: tx %x in block %x", block.ErrTxHashCollision, tx.Hash(), blk.Hash())
		}
	}

	// Second pass: actually write.
	d.blocksByHash[blockHashKey] = blk
	d.blocksByHeight[height] = blk
	for i, tx := range txs {
		hashKey := string(tx.Hash())
		entry, ok := d.txEntries[hashKey]
		if !ok {
			entry = &txEntry{
				tx:        tx,
				instances: make(map[string]*resultInstance),
			}
			d.txEntries[hashKey] = entry
		}
		// Register a pending instance for this block. SetTransactionResults
		// fills bytes later. The (txHash, blockHash) keying means the same
		// tx hash in another block keeps its own instance; pruning one
		// block doesn't disturb others.
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
	// Pre-marshal each result outside the lock. Result.Bytes() may run a
	// proto Marshal — for a 1000-tx block that's ~MB of CPU work, which
	// we don't want to do under the write lock blocking every reader. The
	// extra cost is wasted on the rare error paths (unknown block,
	// count mismatch) but those are exceptional.
	bytesByIdx := make([][]byte, len(results))
	for i, r := range results {
		bytesByIdx[i] = r.Bytes()
	}

	d := m.data
	d.mu.Lock()
	defer d.mu.Unlock()

	blk, ok := d.blocksByHash[string(blockHash)]
	if !ok {
		return fmt.Errorf("%w: %x", block.ErrUnknownBlock, blockHash)
	}
	txs := blk.Transactions()
	if len(txs) != len(bytesByIdx) {
		return fmt.Errorf("%w: block has %d txs, got %d results", block.ErrResultCountMismatch, len(txs), len(bytesByIdx))
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
		inst.bytes = bytesByIdx[i]
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
	// Sort by blockHash so the returned slice has deterministic order
	// across calls — Go map iteration is randomized, and downstream
	// selection (e.g. GigaRouter.Tx tie-breaking on equal heights)
	// depends on stable input order to return the same Result for the
	// same query.
	keys := make([]string, 0, len(entry.instances))
	for k := range entry.instances {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	results := make([]block.Result, 0, len(keys))
	for _, k := range keys {
		inst := entry.instances[k]
		if inst.bytes == nil {
			continue
		}
		// Value-copy the resultInstance: caller gets a fresh slice header
		// pointing at the same backing array. Isolation from a later
		// SetTransactionResults is provided by the fact that
		// SetTransactionResults reassigns inst.bytes (rather than mutating
		// it in place), so the caller's slice header keeps pointing at
		// the old array. See the resultInstance type doc.
		results = append(results, *inst)
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
