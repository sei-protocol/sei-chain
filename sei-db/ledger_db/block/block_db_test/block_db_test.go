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

		for _, tx := range blk.Transactions() {
			got, ok, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, ok, "expected transaction with hash %s", tx.Hash())
			requireBytesEqual(t, tx.Hash(), got.Hash(), "transaction hash")
			requireBytesEqual(t, tx.Bytes(), got.Bytes(), "transaction data")
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

		_, ok, err := db.GetTransactionByHash(ctx, []byte("nonexistent"))
		requireNoError(t, err)
		requireTrue(t, !ok, "expected no transaction with nonexistent hash")
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
			_, ok, err := db.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, ok, "expected transaction %s to survive pruning", tx.Hash())
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
			gotTx, ok, err := db2.GetTransactionByHash(ctx, tx.Hash())
			requireNoError(t, err)
			requireTrue(t, ok, "expected tx to survive close/reopen")
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
				gotTx, ok, err := db.GetTransactionByHash(ctx, expectedTx.Hash())
				requireNoError(t, err)
				requireTrue(t, ok, "tx not found by hash %x (block height %d)", expectedTx.Hash(), expected.Height())
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
