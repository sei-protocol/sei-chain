package blockdb

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

var testRng = crand.NewCannedRandom(4*unit.MB, 42)

type blockDBBuilder struct {
	name    string
	builder func(path string) (BlockDB, error)
}

func buildBuilders() []blockDBBuilder {
	return []blockDBBuilder{
		newMemBlockDBBuilder(),
	}
}

func newMemBlockDBBuilder() blockDBBuilder {
	store := make(map[string]*memBlockDBData)
	return blockDBBuilder{
		name: "mem",
		builder: func(path string) (BlockDB, error) {
			data, ok := store[path]
			if !ok {
				data = &memBlockDBData{
					blocksByHash:   make(map[string]*BinaryBlock),
					blocksByHeight: make(map[uint64]*BinaryBlock),
					txByHash:       make(map[string]*BinaryTransaction),
				}
				store[path] = data
			}
			return &memBlockDB{data: data}, nil
		},
	}
}

func makeBlock(height uint64, numTxs int) *BinaryBlock {
	txs := make([]*BinaryTransaction, numTxs)
	for i := 0; i < numTxs; i++ {
		txs[i] = &BinaryTransaction{
			Hash:        []byte(fmt.Sprintf("tx-%d-%d", height, i)),
			Transaction: []byte(fmt.Sprintf("tx-data-%d-%d", height, i)),
		}
	}
	return &BinaryBlock{
		Height:       height,
		Hash:         []byte(fmt.Sprintf("block-%d", height)),
		BlockData:    []byte(fmt.Sprintf("block-data-%d", height)),
		Transactions: txs,
	}
}

func forEachBuilder(t *testing.T, fn func(t *testing.T, builder func(path string) (BlockDB, error))) {
	for _, b := range buildBuilders() {
		t.Run(b.name, func(t *testing.T) {
			fn(t, b.builder)
		})
	}
}

func TestWriteAndGetBlockByHeight(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		block := makeBlock(1, 2)
		requireNoError(t, db.WriteBlock(ctx, block))

		got, ok, err := db.GetBlockByHeight(ctx, 1)
		requireNoError(t, err)
		requireTrue(t, ok, "expected block at height 1")
		requireBlockEqual(t, block, got)
	})
}

func TestWriteAndGetBlockByHash(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		block := makeBlock(5, 3)
		requireNoError(t, db.WriteBlock(ctx, block))

		got, ok, err := db.GetBlockByHash(ctx, block.Hash)
		requireNoError(t, err)
		requireTrue(t, ok, "expected block with matching hash")
		requireBlockEqual(t, block, got)
	})
}

func TestGetTransactionByHash(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		block := makeBlock(1, 4)
		requireNoError(t, db.WriteBlock(ctx, block))

		for _, tx := range block.Transactions {
			got, ok, err := db.GetTransactionByHash(ctx, tx.Hash)
			requireNoError(t, err)
			requireTrue(t, ok, "expected transaction with hash %s", tx.Hash)
			requireBytesEqual(t, tx.Hash, got.Hash, "transaction hash")
			requireBytesEqual(t, tx.Transaction, got.Transaction, "transaction data")
		}
	})
}

func TestGetBlockNotFound(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
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
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
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
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blocks := make([]*BinaryBlock, 10)
		for i := range blocks {
			blocks[i] = makeBlock(uint64(i+1), 2)
			requireNoError(t, db.WriteBlock(ctx, blocks[i]))
		}

		for _, block := range blocks {
			got, ok, err := db.GetBlockByHeight(ctx, block.Height)
			requireNoError(t, err)
			requireTrue(t, ok, "expected block at height %d", block.Height)
			requireBlockEqual(t, block, got)
		}
	})
}

func TestPrunePreservesUnprunedBlocks(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
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
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		survivingBlock := makeBlock(2, 3)
		requireNoError(t, db.WriteBlock(ctx, makeBlock(1, 1)))
		requireNoError(t, db.WriteBlock(ctx, survivingBlock))

		requireNoError(t, db.Flush(ctx))
		requireNoError(t, db.Prune(ctx, 2))

		for _, tx := range survivingBlock.Transactions {
			_, ok, err := db.GetTransactionByHash(ctx, tx.Hash)
			requireNoError(t, err)
			requireTrue(t, ok, "expected transaction %s to survive pruning", tx.Hash)
		}
	})
}

func TestPruneDoesNotError(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
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
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		path := t.TempDir()

		db, err := builder(path)
		requireNoError(t, err)

		block := makeBlock(1, 2)
		requireNoError(t, db.WriteBlock(ctx, block))
		requireNoError(t, db.Flush(ctx))
		requireNoError(t, db.Close(ctx))

		db2, err := builder(path)
		requireNoError(t, err)
		defer db2.Close(ctx)

		got, ok, err := db2.GetBlockByHeight(ctx, 1)
		requireNoError(t, err)
		requireTrue(t, ok, "expected block to survive close/reopen")
		requireBlockEqual(t, block, got)

		for _, tx := range block.Transactions {
			gotTx, ok, err := db2.GetTransactionByHash(ctx, tx.Hash)
			requireNoError(t, err)
			requireTrue(t, ok, "expected tx to survive close/reopen")
			requireBytesEqual(t, tx.Transaction, gotTx.Transaction, "transaction data")
		}
	})
}

func TestCloseAndReopenThenWrite(t *testing.T) {
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
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
	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
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

	forEachBuilder(t, func(t *testing.T, builder func(string) (BlockDB, error)) {
		ctx := context.Background()
		db, err := builder(t.TempDir())
		requireNoError(t, err)
		defer db.Close(ctx)

		blocks := make([]*BinaryBlock, numBlocks)
		for i := range blocks {
			blocks[i] = makeRandomBlock(testRng, uint64(i+1), txsPerBlock)
			requireNoError(t, db.WriteBlock(ctx, blocks[i]))
		}

		requireNoError(t, db.Flush(ctx))

		for _, expected := range blocks {
			byHeight, ok, err := db.GetBlockByHeight(ctx, expected.Height)
			requireNoError(t, err)
			requireTrue(t, ok, "block not found by height %d", expected.Height)
			requireBlockBytesEqual(t, expected, byHeight)

			byHash, ok, err := db.GetBlockByHash(ctx, expected.Hash)
			requireNoError(t, err)
			requireTrue(t, ok, "block not found by hash at height %d", expected.Height)
			requireBlockBytesEqual(t, expected, byHash)

			for _, expectedTx := range expected.Transactions {
				gotTx, ok, err := db.GetTransactionByHash(ctx, expectedTx.Hash)
				requireNoError(t, err)
				requireTrue(t, ok, "tx not found by hash %x (block height %d)", expectedTx.Hash, expected.Height)
				requireBytesEqual(t, expectedTx.Hash, gotTx.Hash, "tx hash")
				requireBytesEqual(t, expectedTx.Transaction, gotTx.Transaction, "tx data")
			}
		}
	})
}

// makeRandomBlock builds a block with deterministic random binary payloads.
// Returned slices are owned copies safe for storage and later comparison.
func makeRandomBlock(rng *crand.CannedRandom, height uint64, numTxs int) *BinaryBlock {
	txs := make([]*BinaryTransaction, numTxs)
	for i := range txs {
		txHash := rng.Address('t', int64(height)*1000+int64(i), 32)
		txDataLen := 64 + int(rng.Int64Range(0, 512))
		txData := copyBytes(rng.Bytes(txDataLen))
		txs[i] = &BinaryTransaction{Hash: txHash, Transaction: txData}
	}

	blockHash := rng.Address('b', int64(height), 32)
	blockDataLen := 128 + int(rng.Int64Range(0, 1024))
	blockData := copyBytes(rng.Bytes(blockDataLen))

	return &BinaryBlock{
		Height:       height,
		Hash:         blockHash,
		BlockData:    blockData,
		Transactions: txs,
	}
}

func copyBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// requireBlockBytesEqual does a deep byte-level comparison, suitable for verifying
// round-trip fidelity through serialization.
func requireBlockBytesEqual(t *testing.T, expected, actual *BinaryBlock) {
	t.Helper()
	if expected.Height != actual.Height {
		t.Fatalf("height mismatch: expected %d, got %d", expected.Height, actual.Height)
	}
	requireBytesEqual(t, expected.Hash, actual.Hash, "block hash")
	requireBytesEqual(t, expected.BlockData, actual.BlockData, "block data")
	if len(expected.Transactions) != len(actual.Transactions) {
		t.Fatalf("transaction count mismatch at height %d: expected %d, got %d",
			expected.Height, len(expected.Transactions), len(actual.Transactions))
	}
	for i, tx := range expected.Transactions {
		label := fmt.Sprintf("height %d tx[%d]", expected.Height, i)
		requireBytesEqual(t, tx.Hash, actual.Transactions[i].Hash, label+" hash")
		requireBytesEqual(t, tx.Transaction, actual.Transactions[i].Transaction, label+" data")
	}
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

func requireBlockEqual(t *testing.T, expected, actual *BinaryBlock) {
	t.Helper()
	if expected.Height != actual.Height {
		t.Fatalf("height mismatch: expected %d, got %d", expected.Height, actual.Height)
	}
	requireBytesEqual(t, expected.Hash, actual.Hash, "block hash")
	requireBytesEqual(t, expected.BlockData, actual.BlockData, "block data")
	if len(expected.Transactions) != len(actual.Transactions) {
		t.Fatalf("transaction count mismatch: expected %d, got %d",
			len(expected.Transactions), len(actual.Transactions))
	}
	for i, tx := range expected.Transactions {
		requireBytesEqual(t, tx.Hash, actual.Transactions[i].Hash, fmt.Sprintf("tx[%d] hash", i))
		requireBytesEqual(t, tx.Transaction, actual.Transactions[i].Transaction, fmt.Sprintf("tx[%d] data", i))
	}
}
