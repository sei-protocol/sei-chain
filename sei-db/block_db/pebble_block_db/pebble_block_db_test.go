package pebbleblockdb

import (
	"context"
	"fmt"
	"testing"

	blockdb "github.com/sei-protocol/sei-chain/sei-db/block_db"
)

func makeBlock(height uint64, numTxs int) *blockdb.BinaryBlock {
	txs := make([]*blockdb.BinaryTransaction, numTxs)
	for i := 0; i < numTxs; i++ {
		txs[i] = &blockdb.BinaryTransaction{
			Hash:        []byte(fmt.Sprintf("tx-%d-%d", height, i)),
			Transaction: []byte(fmt.Sprintf("tx-data-%d-%d", height, i)),
		}
	}
	return &blockdb.BinaryBlock{
		Height:       height,
		Hash:         []byte(fmt.Sprintf("block-%d", height)),
		BlockData:    []byte(fmt.Sprintf("block-data-%d", height)),
		Transactions: txs,
	}
}

func openTestDB(t *testing.T) *pebbleBlockDB {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	return db
}

func TestPruneRemovesBlocks(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for i := uint64(1); i <= 10; i++ {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(i, 2)))
	}
	requireNoError(t, db.Flush(ctx))
	requireNoError(t, db.Prune(ctx, 6))
	// Flush again to ensure the prune command has been fully processed.
	requireNoError(t, db.Flush(ctx))

	for i := uint64(1); i <= 5; i++ {
		_, ok, err := db.GetBlockByHeight(ctx, i)
		requireNoError(t, err)
		if ok {
			t.Fatalf("block at height %d should have been pruned", i)
		}

		_, ok, err = db.GetBlockByHash(ctx, []byte(fmt.Sprintf("block-%d", i)))
		requireNoError(t, err)
		if ok {
			t.Fatalf("block hash index for height %d should have been pruned", i)
		}
	}

	for i := uint64(6); i <= 10; i++ {
		_, ok, err := db.GetBlockByHeight(ctx, i)
		requireNoError(t, err)
		if !ok {
			t.Fatalf("block at height %d should have survived pruning", i)
		}
	}
}

func TestPruneRemovesTransactions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for i := uint64(1); i <= 5; i++ {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(i, 3)))
	}
	requireNoError(t, db.Flush(ctx))
	requireNoError(t, db.Prune(ctx, 3))
	requireNoError(t, db.Flush(ctx))

	// Transactions from pruned blocks (heights 1-2) should be gone.
	for h := uint64(1); h <= 2; h++ {
		for j := 0; j < 3; j++ {
			txHash := []byte(fmt.Sprintf("tx-%d-%d", h, j))
			_, ok, err := db.GetTransactionByHash(ctx, txHash)
			requireNoError(t, err)
			if ok {
				t.Fatalf("tx %s should have been pruned", txHash)
			}
		}
	}

	// Transactions from surviving blocks (heights 3-5) should remain.
	for h := uint64(3); h <= 5; h++ {
		for j := 0; j < 3; j++ {
			txHash := []byte(fmt.Sprintf("tx-%d-%d", h, j))
			_, ok, err := db.GetTransactionByHash(ctx, txHash)
			requireNoError(t, err)
			if !ok {
				t.Fatalf("tx %s should have survived pruning", txHash)
			}
		}
	}
}

func TestOutOfOrderHeightWrites(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	// Write heights out of order to exercise the atomic lo/hi CAS paths.
	for _, h := range []uint64{5, 3, 8, 1, 10, 2} {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(h, 1)))
	}
	requireNoError(t, db.Flush(ctx))

	for _, h := range []uint64{1, 2, 3, 5, 8, 10} {
		_, ok, err := db.GetBlockByHeight(ctx, h)
		requireNoError(t, err)
		if !ok {
			t.Fatalf("block at height %d not found", h)
		}
	}

	// Heights not written should be absent.
	for _, h := range []uint64{4, 6, 7, 9} {
		_, ok, err := db.GetBlockByHeight(ctx, h)
		requireNoError(t, err)
		if ok {
			t.Fatalf("block at height %d should not exist", h)
		}
	}
}

func TestPruneAfterReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := Open(ctx, dir)
	requireNoError(t, err)

	for i := uint64(1); i <= 10; i++ {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(i, 1)))
	}
	requireNoError(t, db.Flush(ctx))
	requireNoError(t, db.Close(ctx))

	// Reopen, prune, verify metadata was restored correctly.
	db, err = Open(ctx, dir)
	requireNoError(t, err)
	defer db.Close(ctx)

	requireNoError(t, db.Prune(ctx, 6))
	requireNoError(t, db.Flush(ctx))

	for i := uint64(1); i <= 5; i++ {
		_, ok, err := db.GetBlockByHeight(ctx, i)
		requireNoError(t, err)
		if ok {
			t.Fatalf("block at height %d should have been pruned after reopen", i)
		}
	}
	for i := uint64(6); i <= 10; i++ {
		_, ok, err := db.GetBlockByHeight(ctx, i)
		requireNoError(t, err)
		if !ok {
			t.Fatalf("block at height %d should have survived prune after reopen", i)
		}
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
