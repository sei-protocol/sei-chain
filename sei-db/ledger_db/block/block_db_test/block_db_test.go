package blockdbtest

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	memblockdb "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/mem_block_db"
)

var testRng = crand.NewCannedRandom(4*unit.MB, 42)

type blockDBBuilder struct {
	name    string
	builder func(path string) (block.BlockDB, error)
}

func buildBuilders() []blockDBBuilder {
	return []blockDBBuilder{
		newMemBlockDBBuilder(),
	}
}

func newMemBlockDBBuilder() blockDBBuilder {
	db := memblockdb.NewMemBlockDB()
	return blockDBBuilder{
		name: "mem",
		builder: func(_ string) (block.BlockDB, error) {
			return db, nil
		},
	}
}

type testTx struct {
	hash  []byte
	bytes []byte
}

func (t *testTx) Hash() []byte  { return t.hash }
func (t *testTx) Bytes() []byte { return t.bytes }

type testResult struct {
	bytes  []byte
	height uint64
	index  uint32
}

func (r testResult) Bytes() []byte  { return r.bytes }
func (r testResult) Height() uint64 { return r.height }
func (r testResult) Index() uint32  { return r.index }

type testBlock struct {
	hash   []byte
	height uint64
	time   time.Time
	txs    []block.Transaction
}

func (b *testBlock) Hash() []byte                      { return b.hash }
func (b *testBlock) Height() uint64                    { return b.height }
func (b *testBlock) Time() time.Time                   { return b.time }
func (b *testBlock) Transactions() []block.Transaction { return b.txs }

func makeBlock(height uint64, numTxs int) *testBlock {
	txs := make([]block.Transaction, numTxs)
	for i := 0; i < numTxs; i++ {
		txs[i] = &testTx{
			hash:  []byte(fmt.Sprintf("tx-%d-%d", height, i)),
			bytes: []byte(fmt.Sprintf("tx-data-%d-%d", height, i)),
		}
	}
	return &testBlock{
		hash:   []byte(fmt.Sprintf("block-%d", height)),
		height: height,
		txs:    txs,
	}
}

// makeResults builds a testResult per tx, populated with synthetic bytes
// + the canonical (height, index) for that block's tx slice.
func makeResults(blk *testBlock) []block.Result {
	txs := blk.Transactions()
	out := make([]block.Result, len(txs))
	for i := range txs {
		out[i] = testResult{
			bytes:  []byte(fmt.Sprintf("result-%d-%d", blk.height, i)),
			height: blk.height,
			index:  uint32(i), //nolint:gosec
		}
	}
	return out
}

func forEachBuilder(t *testing.T, fn func(t *testing.T, builder func(path string) (block.BlockDB, error))) {
	for _, b := range buildBuilders() {
		t.Run(b.name, func(t *testing.T) {
			fn(t, b.builder)
		})
	}
}

func TestWriteAndGetBlockByHeight(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(1, 2)
		requireNoError(t, db.WriteBlock(ctx, blk))

		got, ok, err := db.GetBlockByHeight(ctx, 1)
		requireNoError(t, err)
		requireTrue(t, ok, "expected block at height 1")
		requireBlockEqual(t, blk, got)
	})
}

func TestWriteAndGetBlockByHash(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(5, 3)
		requireNoError(t, db.WriteBlock(ctx, blk))

		got, ok, err := db.GetBlockByHash(ctx, blk.Hash())
		requireNoError(t, err)
		requireTrue(t, ok, "expected block with matching hash")
		requireBlockEqual(t, blk, got)
	})
}

func TestGetTransactionByHash(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(1, 4)
		requireNoError(t, db.WriteBlock(ctx, blk))

		// Block written, no results attached yet — found=true with empty results.
		for _, tx := range blk.Transactions() {
			gotTx, results, found, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected tx %s found pre-results", tx.Hash())
			requireBytesEqual(t, tx.Hash(), gotTx.Hash(), "transaction hash")
			requireBytesEqual(t, tx.Bytes(), gotTx.Bytes(), "transaction data")
			requireTrue(t, len(results) == 0, "expected 0 results pre-SetTransactionResults, got %d", len(results))
		}
	})
}

func TestGetBlockNotFound(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		_, ok, err := db.GetBlockByHeight(ctx, 999)
		requireNoError(t, err)
		requireTrue(t, !ok, "expected no block at height 999")

		_, ok, err = db.GetBlockByHash(ctx, []byte("nonexistent"))
		requireNoError(t, err)
		requireTrue(t, !ok, "expected no block with nonexistent hash")
	})
}

func TestGetTransactionNotFound(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		_, _, found, err := db.GetTransactionByHash(ctx, []byte("nonexistent"))
		requireNoError(t, err)
		requireTrue(t, !found, "expected no transaction with nonexistent hash")
	})
}

func TestMultipleBlocks(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blocks := make([]*testBlock, 10)
		for i := range blocks {
			blocks[i] = makeBlock(uint64(i+1), 2)
			requireNoError(t, db.WriteBlock(ctx, blocks[i]))
		}

		for _, blk := range blocks {
			got, ok, err := db.GetBlockByHeight(ctx, blk.Height())
			requireNoError(t, err)
			requireTrue(t, ok, "expected block at height %d", blk.Height())
			requireBlockEqual(t, blk, got)
		}
	})
}

func TestSetTransactionResults(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(7, 3)
		requireNoError(t, db.WriteBlock(ctx, blk))

		// Pre-results: found=true, results empty.
		for _, tx := range blk.Transactions() {
			gotTx, results, found, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected tx %s found pre-results", tx.Hash())
			requireBytesEqual(t, tx.Bytes(), gotTx.Bytes(), "tx body bytes")
			requireTrue(t, len(results) == 0, "expected 0 results pre-SetTransactionResults, got %d", len(results))
		}

		// Attach results.
		results := makeResults(blk)
		requireNoError(t, db.SetTransactionResults(ctx, blk.Hash(), results))

		// Post-results: results carries (bytes, height, index).
		for i, tx := range blk.Transactions() {
			gotTx, gotResults, found, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected tx %s found post-results", tx.Hash())
			requireBytesEqual(t, tx.Bytes(), gotTx.Bytes(), "tx body bytes")
			requireTrue(t, len(gotResults) == 1, "expected 1 result, got %d", len(gotResults))
			r := gotResults[0]
			requireBytesEqual(t, results[i].Bytes(), r.Bytes(), fmt.Sprintf("tx[%d] result bytes", i))
			requireTrue(t, r.Height() == 7, "expected result height 7, got %d", r.Height())
			requireTrue(t, r.Index() == uint32(i), "expected result index %d, got %d", i, r.Index()) //nolint:gosec
		}
	})
}

// TestTransactionMultipleBlocks pins the (txHash, blockHash) dedup behavior:
// the same tx hash included in two different blocks is recorded as two
// separate Result entries. Both remain reachable while both blocks are
// retained; pruning either one leaves the other (and its result)
// reachable. Models the lane-block scenario where the same tx body
// appears in two different GlobalBlocks (one per lane).
func TestTransactionMultipleBlocks(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		// Two blocks at different heights with the same tx hash + bytes.
		const txHash = "shared-tx-hash"
		const txBytes = "shared-tx-data"
		shared := func() block.Transaction {
			return &testTx{hash: []byte(txHash), bytes: []byte(txBytes)}
		}
		blkA := &testBlock{
			hash:   []byte("block-A"),
			height: 1,
			txs:    []block.Transaction{shared()},
		}
		blkB := &testBlock{
			hash:   []byte("block-B"),
			height: 2,
			txs:    []block.Transaction{shared()},
		}
		requireNoError(t, db.WriteBlock(ctx, blkA))
		requireNoError(t, db.WriteBlock(ctx, blkB))

		// Both blocks present, no results yet → found, empty results.
		_, results, found, err := db.GetTransactionByHash(ctx, []byte(txHash))
		requireNoError(t, err)
		requireTrue(t, found, "expected found")
		requireTrue(t, len(results) == 0, "expected 0 results pre-SetTransactionResults, got %d", len(results))

		// Attach results: A gets "result-A", B gets "result-B".
		requireNoError(t, db.SetTransactionResults(ctx, blkA.Hash(), []block.Result{
			testResult{bytes: []byte("result-A"), height: 1, index: 0},
		}))
		requireNoError(t, db.SetTransactionResults(ctx, blkB.Hash(), []block.Result{
			testResult{bytes: []byte("result-B"), height: 2, index: 0},
		}))

		// Both results reachable.
		_, results, found, err = db.GetTransactionByHash(ctx, []byte(txHash))
		requireNoError(t, err)
		requireTrue(t, found, "expected found")
		requireTrue(t, len(results) == 2, "expected 2 results, got %d", len(results))
		seen := map[string]uint64{}
		for _, r := range results {
			seen[string(r.Bytes())] = r.Height()
		}
		requireTrue(t, seen["result-A"] == 1, "expected result-A at height 1, got %v", seen["result-A"])
		requireTrue(t, seen["result-B"] == 2, "expected result-B at height 2, got %v", seen["result-B"])

		// Prune block A; B's result remains reachable.
		requireNoError(t, db.Prune(ctx, 2))
		_, results, found, err = db.GetTransactionByHash(ctx, []byte(txHash))
		requireNoError(t, err)
		requireTrue(t, found, "expected found after prune A")
		requireTrue(t, len(results) == 1, "expected 1 result after prune A, got %d", len(results))
		requireBytesEqual(t, []byte("result-B"), results[0].Bytes(), "remaining result must be B")
		requireTrue(t, results[0].Height() == 2, "remaining result height must be 2")

		// Prune block B; tx is now unknown (entire entry collected).
		requireNoError(t, db.Prune(ctx, 3))
		_, _, found, err = db.GetTransactionByHash(ctx, []byte(txHash))
		requireNoError(t, err)
		requireTrue(t, !found, "expected tx unknown after pruning all blocks containing it")
	})
}

// TestSetTransactionResultsOverwrites pins the documented "second call
// overwrites" behavior — useful for callers that re-execute a block on
// recovery and expect the latest results to win.
func TestSetTransactionResultsOverwrites(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(3, 2)
		requireNoError(t, db.WriteBlock(ctx, blk))

		// First attach: "old-N".
		first := []block.Result{
			testResult{bytes: []byte("old-0"), height: 3, index: 0},
			testResult{bytes: []byte("old-1"), height: 3, index: 1},
		}
		requireNoError(t, db.SetTransactionResults(ctx, blk.Hash(), first))

		// Second attach: "new-N" — must replace.
		second := []block.Result{
			testResult{bytes: []byte("new-0"), height: 3, index: 0},
			testResult{bytes: []byte("new-1"), height: 3, index: 1},
		}
		requireNoError(t, db.SetTransactionResults(ctx, blk.Hash(), second))

		for i, tx := range blk.Transactions() {
			_, results, found, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected tx %s found", tx.Hash())
			requireTrue(t, len(results) == 1, "expected 1 result, got %d", len(results))
			requireBytesEqual(t, second[i].Bytes(), results[0].Bytes(), fmt.Sprintf("tx[%d] result must reflect overwrite", i))
		}
	})
}

// TestWriteBlockIdempotent pins the contract that calling WriteBlock a
// second time for the same blockHash is a silent no-op — does NOT wipe
// any results already attached via SetTransactionResults. Without this
// the second WriteBlock would silently corrupt the index by re-creating
// pending instances on top of recorded ones (review finding 1).
func TestWriteBlockIdempotent(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(4, 2)
		requireNoError(t, db.WriteBlock(ctx, blk))
		requireNoError(t, db.SetTransactionResults(ctx, blk.Hash(), makeResults(blk)))

		// Second WriteBlock for the same block — must not destroy results.
		requireNoError(t, db.WriteBlock(ctx, blk))

		for i, tx := range blk.Transactions() {
			_, results, found, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected tx %s found after re-WriteBlock", tx.Hash())
			requireTrue(t, len(results) == 1, "expected 1 result after re-WriteBlock, got %d", len(results))
			requireBytesEqual(t, []byte(fmt.Sprintf("result-%d-%d", 4, i)), results[0].Bytes(), fmt.Sprintf("tx[%d] result must survive re-WriteBlock", i))
		}
	})
}

// TestWriteBlockTxHashCollision pins the defensive collision check from
// review finding 6: writing a second block whose tx hash matches a
// previously-written tx but with different bytes is rejected loudly,
// rather than silently keeping the first-writer's bytes.
func TestWriteBlockTxHashCollision(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blkA := &testBlock{
			hash:   []byte("block-A"),
			height: 1,
			txs: []block.Transaction{
				&testTx{hash: []byte("h"), bytes: []byte("v1")},
			},
		}
		blkB := &testBlock{
			hash:   []byte("block-B"),
			height: 2,
			txs: []block.Transaction{
				&testTx{hash: []byte("h"), bytes: []byte("v2")},
			},
		}
		requireNoError(t, db.WriteBlock(ctx, blkA))
		err = db.WriteBlock(ctx, blkB)
		requireTrue(t, err != nil, "expected ErrTxHashCollision for second block with mismatched bytes")

		// Block B must not have been recorded — partial state from a
		// failed validation would corrupt blocksByHash.
		_, ok, err := db.GetBlockByHash(ctx, blkB.Hash())
		requireNoError(t, err)
		requireTrue(t, !ok, "block B must not be present after collision rejection")
	})
}

// TestGetTransactionByHashDeterministicOrder pins the sort-by-blockHash
// behavior on the read path: with multiple instances, the returned
// slice must be in stable order across repeated calls — otherwise
// downstream selection that ties on Height() would non-deterministically
// flip between RPC calls (review finding 2).
func TestGetTransactionByHashDeterministicOrder(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		const txHash = "shared"
		const txBytes = "data"
		shared := func() block.Transaction {
			return &testTx{hash: []byte(txHash), bytes: []byte(txBytes)}
		}
		// Three blocks at the same height carrying the same tx — exercises
		// the tie-breaker path. Block hashes intentionally chosen so that
		// lexicographic order doesn't match insertion order.
		blkB := &testBlock{hash: []byte("bbb"), height: 5, txs: []block.Transaction{shared()}}
		blkA := &testBlock{hash: []byte("aaa"), height: 5, txs: []block.Transaction{shared()}}
		blkC := &testBlock{hash: []byte("ccc"), height: 5, txs: []block.Transaction{shared()}}
		requireNoError(t, db.WriteBlock(ctx, blkB))
		requireNoError(t, db.WriteBlock(ctx, blkA))
		requireNoError(t, db.WriteBlock(ctx, blkC))

		requireNoError(t, db.SetTransactionResults(ctx, blkB.Hash(), []block.Result{testResult{bytes: []byte("rB"), height: 5, index: 0}}))
		requireNoError(t, db.SetTransactionResults(ctx, blkA.Hash(), []block.Result{testResult{bytes: []byte("rA"), height: 5, index: 0}}))
		requireNoError(t, db.SetTransactionResults(ctx, blkC.Hash(), []block.Result{testResult{bytes: []byte("rC"), height: 5, index: 0}}))

		// Repeatedly read; results must be in the same order each time.
		var first [][]byte
		for iter := 0; iter < 10; iter++ {
			_, results, found, err := db.GetTransactionByHash(ctx, []byte(txHash))
			requireNoError(t, err)
			requireTrue(t, found, "expected tx found")
			requireTrue(t, len(results) == 3, "expected 3 results, got %d", len(results))
			gotOrder := make([][]byte, len(results))
			for i, r := range results {
				gotOrder[i] = r.Bytes()
			}
			if iter == 0 {
				first = gotOrder
				continue
			}
			for i := range gotOrder {
				requireBytesEqual(t, first[i], gotOrder[i], fmt.Sprintf("iter %d position %d", iter, i))
			}
		}
	})
}

// TestGetTransactionByHashReadIsolation pins review finding 3: a Result
// returned by an earlier call must not be mutated by a later
// SetTransactionResults overwrite (the documented "second call
// replaces" behavior). Without the deep-copy of bytes on read, the
// caller's Result.Bytes() would observe the new value retroactively.
func TestGetTransactionByHashReadIsolation(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blk := makeBlock(7, 1)
		requireNoError(t, db.WriteBlock(ctx, blk))
		requireNoError(t, db.SetTransactionResults(ctx, blk.Hash(), []block.Result{
			testResult{bytes: []byte("first"), height: 7, index: 0},
		}))

		_, results, _, err := db.GetTransactionByHash(ctx, blk.Transactions()[0].Hash())
		requireNoError(t, err)
		requireTrue(t, len(results) == 1, "expected 1 result")
		held := results[0]

		// Overwrite — caller's earlier read must not be mutated.
		requireNoError(t, db.SetTransactionResults(ctx, blk.Hash(), []block.Result{
			testResult{bytes: []byte("second"), height: 7, index: 0},
		}))
		requireBytesEqual(t, []byte("first"), held.Bytes(), "earlier-read Result must not observe overwrite")
	})
}

func TestSetTransactionResultsErrors(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		// Unknown block hash.
		err = db.SetTransactionResults(ctx, []byte("nonexistent"), nil)
		requireTrue(t, err != nil, "expected error for unknown block hash")

		// Mismatched count.
		blk := makeBlock(1, 2)
		requireNoError(t, db.WriteBlock(ctx, blk))
		err = db.SetTransactionResults(ctx, blk.Hash(), []block.Result{testResult{bytes: []byte("only-one"), height: 1, index: 0}})
		requireTrue(t, err != nil, "expected error for mismatched result count")
	})
}

func TestPrunePreservesUnprunedBlocks(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		for i := uint64(1); i <= 10; i++ {
			requireNoError(t, db.WriteBlock(ctx, makeBlock(i, 1)))
		}

		requireNoError(t, db.Flush(ctx))
		requireNoError(t, db.Prune(ctx, 6))

		for i := uint64(6); i <= 10; i++ {
			_, ok, err := db.GetBlockByHeight(ctx, i)
			requireNoError(t, err)
			requireTrue(t, ok, "expected block at height %d to survive pruning", i)
		}
	})
}

func TestPrunePreservesUnprunedTransactions(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		survivingBlock := makeBlock(2, 3)
		requireNoError(t, db.WriteBlock(ctx, makeBlock(1, 1)))
		requireNoError(t, db.WriteBlock(ctx, survivingBlock))

		requireNoError(t, db.Flush(ctx))
		requireNoError(t, db.Prune(ctx, 2))

		for _, tx := range survivingBlock.Transactions() {
			_, _, found, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected transaction %s to survive pruning", tx.Hash())
		}
	})
}

func TestPruneDoesNotError(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		requireNoError(t, db.Prune(ctx, 100))

		for i := uint64(1); i <= 5; i++ {
			requireNoError(t, db.WriteBlock(ctx, makeBlock(i, 1)))
		}

		requireNoError(t, db.Prune(ctx, 3))
		requireNoError(t, db.Prune(ctx, 100))
	})
}

func TestCloseAndReopen(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		path := t.TempDir()

		db, err := builder(path)
		requireNoError(t, err)

		blk := makeBlock(1, 2)
		requireNoError(t, db.WriteBlock(ctx, blk))
		requireNoError(t, db.Flush(ctx))
		requireNoError(t, db.Close(ctx))

		db2, err := builder(path)
		requireNoError(t, err)
		defer db2.Close(ctx)

		got, ok, err := db2.GetBlockByHeight(ctx, 1)
		requireNoError(t, err)
		requireTrue(t, ok, "expected block to survive close/reopen")
		requireBlockEqual(t, blk, got)

		for _, tx := range blk.Transactions() {
			gotTx, _, found, err := db2.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, found, "expected tx to survive close/reopen")
			requireBytesEqual(t, tx.Bytes(), gotTx.Bytes(), "transaction data")
		}
	})
}

func TestCloseAndReopenThenWrite(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		path := t.TempDir()

		db, err := builder(path)
		requireNoError(t, err)
		requireNoError(t, db.WriteBlock(ctx, makeBlock(1, 1)))
		requireNoError(t, db.Flush(ctx))
		requireNoError(t, db.Close(ctx))

		db2, err := builder(path)
		requireNoError(t, err)
		defer db2.Close(ctx)

		requireNoError(t, db2.WriteBlock(ctx, makeBlock(2, 1)))

		for _, h := range []uint64{1, 2} {
			_, ok, err := db2.GetBlockByHeight(ctx, h)
			requireNoError(t, err)
			requireTrue(t, ok, "expected block at height %d after reopen+write", h)
		}
	})
}

func TestFlush(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		requireNoError(t, db.Flush(ctx))

		requireNoError(t, db.WriteBlock(ctx, makeBlock(1, 1)))
		requireNoError(t, db.Flush(ctx))
	})
}

func TestBulkWriteAndQuery(t *testing.T) {
	const numBlocks = 1000
	const txsPerBlock = 50

	forEachBuilder(t, func(t *testing.T, builder func(string) (block.BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blocks := make([]*testBlock, numBlocks)
		for i := range blocks {
			blocks[i] = makeRandomBlock(testRng, uint64(i+1), txsPerBlock)
			requireNoError(t, db.WriteBlock(ctx, blocks[i]))
		}

		requireNoError(t, db.Flush(ctx))

		for _, expected := range blocks {
			byHeight, ok, err := db.GetBlockByHeight(ctx, expected.Height())
			requireNoError(t, err)
			requireTrue(t, ok, "block not found by height %d", expected.Height())
			requireBlockEqual(t, expected, byHeight)

			byHash, ok, err := db.GetBlockByHash(ctx, expected.Hash())
			requireNoError(t, err)
			requireTrue(t, ok, "block not found by hash at height %d", expected.Height())
			requireBlockEqual(t, expected, byHash)

			for _, expectedTx := range expected.Transactions() {
				gotTx, _, found, err := db.GetTransactionByHash(ctx, expectedTx.Hash())
				requireNoError(t, err)
				requireTrue(t, found, "tx not found by hash %x (block height %d)", expectedTx.Hash(), expected.Height())
				requireBytesEqual(t, expectedTx.Hash(), gotTx.Hash(), "tx hash")
				requireBytesEqual(t, expectedTx.Bytes(), gotTx.Bytes(), "tx data")
			}
		}
	})
}

// makeRandomBlock builds a block with deterministic random binary payloads.
// Returned slices are owned copies safe for storage and later comparison.
func makeRandomBlock(rng *crand.CannedRandom, height uint64, numTxs int) *testBlock {
	txs := make([]block.Transaction, numTxs)
	for i := range txs {
		txHash := rng.Address('t', int64(height)*1000+int64(i), 32)
		txDataLen := 64 + int(rng.Int64Range(0, 512))
		txData := copyBytes(rng.Bytes(txDataLen))
		txs[i] = &testTx{hash: txHash, bytes: txData}
	}

	blockHash := rng.Address('b', int64(height), 32)
	return &testBlock{
		hash:   blockHash,
		height: height,
		txs:    txs,
	}
}

func copyBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// --- test helpers ---

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireTrue(t *testing.T, cond bool, format string, args ...any) {
	t.Helper()
	if !cond {
		t.Fatalf(format, args...)
	}
}

func requireBytesEqual(t *testing.T, expected, actual []byte, label string) {
	t.Helper()
	if !bytes.Equal(expected, actual) {
		t.Fatalf("%s mismatch: expected %q, got %q", label, expected, actual)
	}
}

func requireBlockEqual(t *testing.T, expected, actual block.Block) {
	t.Helper()
	if expected.Height() != actual.Height() {
		t.Fatalf("height mismatch: expected %d, got %d", expected.Height(), actual.Height())
	}
	requireBytesEqual(t, expected.Hash(), actual.Hash(), "block hash")
	expTxs := expected.Transactions()
	actTxs := actual.Transactions()
	if len(expTxs) != len(actTxs) {
		t.Fatalf("transaction count mismatch: expected %d, got %d", len(expTxs), len(actTxs))
	}
	for i, tx := range expTxs {
		requireBytesEqual(t, tx.Hash(), actTxs[i].Hash(), fmt.Sprintf("tx[%d] hash", i))
		requireBytesEqual(t, tx.Bytes(), actTxs[i].Bytes(), fmt.Sprintf("tx[%d] data", i))
	}
}
