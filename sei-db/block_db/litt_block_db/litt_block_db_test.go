package littblockdb

import (
	"bytes"
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

func openTestDB(t *testing.T) *littBlockDB {
	t.Helper()
	db, err := NewLittBlockDB(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	return db
}

func TestCodecRoundTrip(t *testing.T) {
	block := makeBlock(42, 3)
	value, skeys := marshalBlock(block)

	got, err := unmarshalBlock(value)
	requireNoError(t, err)
	requireBlockEqual(t, block, got)

	// 1 block hash + 3 tx hashes
	if len(skeys) != 4 {
		t.Fatalf("expected 4 secondary keys, got %d", len(skeys))
	}

	// Each tx secondary key should point to the correct sub-range of the value.
	for i, tx := range block.Transactions {
		sk := skeys[i]
		slice := value[sk.Offset : sk.Offset+sk.Length]
		if !bytes.Equal(slice, tx.Transaction) {
			t.Fatalf("tx %d secondary key sub-range mismatch: got %q, want %q", i, slice, tx.Transaction)
		}
	}

	// Block hash secondary key should alias the full value.
	bsk := skeys[len(skeys)-1]
	if bsk.Offset != 0 || bsk.Length != uint32(len(value)) { //nolint:gosec
		t.Fatalf("block hash secondary key: offset=%d length=%d, want 0/%d", bsk.Offset, bsk.Length, len(value))
	}
}

func TestWriteAndReadByHeight(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	block := makeBlock(1, 2)
	requireNoError(t, db.WriteBlock(ctx, block))

	got, ok, err := db.GetBlockByHeight(ctx, 1)
	requireNoError(t, err)
	requireTrue(t, ok, "expected block at height 1")
	requireBlockEqual(t, block, got)
}

func TestWriteAndReadByHash(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	block := makeBlock(5, 3)
	requireNoError(t, db.WriteBlock(ctx, block))

	got, ok, err := db.GetBlockByHash(ctx, block.Hash)
	requireNoError(t, err)
	requireTrue(t, ok, "expected block by hash")
	requireBlockEqual(t, block, got)
}

func TestWriteAndReadTxByHash(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	block := makeBlock(1, 4)
	requireNoError(t, db.WriteBlock(ctx, block))

	for _, tx := range block.Transactions {
		got, ok, err := db.GetTransactionByHash(ctx, tx.Hash)
		requireNoError(t, err)
		requireTrue(t, ok, "expected tx %s", tx.Hash)
		requireBytesEqual(t, tx.Hash, got.Hash, "tx hash")
		requireBytesEqual(t, tx.Transaction, got.Transaction, "tx data")
	}
}

func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	_, ok, err := db.GetBlockByHeight(ctx, 999)
	requireNoError(t, err)
	requireTrue(t, !ok, "expected no block at height 999")

	_, ok, err = db.GetBlockByHash(ctx, []byte("nonexistent"))
	requireNoError(t, err)
	requireTrue(t, !ok, "expected no block with nonexistent hash")

	_, ok, err = db.GetTransactionByHash(ctx, []byte("nonexistent"))
	requireNoError(t, err)
	requireTrue(t, !ok, "expected no tx with nonexistent hash")
}

func TestOutOfOrderHeightWrites(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for _, h := range []uint64{5, 3, 8, 1, 10, 2} {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(h, 1)))
	}

	lo, err := db.GetLowestBlockHeight(ctx)
	requireNoError(t, err)
	if lo != 1 {
		t.Fatalf("expected lowest 1, got %d", lo)
	}

	hi, err := db.GetHighestBlockHeight(ctx)
	requireNoError(t, err)
	if hi != 10 {
		t.Fatalf("expected highest 10, got %d", hi)
	}
}

func TestGetBlockHeightsEmpty(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	_, err := db.GetLowestBlockHeight(ctx)
	if err == nil {
		t.Fatal("expected error on empty db, got nil")
	}
	_, err = db.GetHighestBlockHeight(ctx)
	if err == nil {
		t.Fatal("expected error on empty db, got nil")
	}
}

func TestCloseAndReopenPreservesData(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := NewLittBlockDB(dir, 0)
	requireNoError(t, err)

	block := makeBlock(1, 2)
	requireNoError(t, db.WriteBlock(ctx, block))
	requireNoError(t, db.Flush(ctx))
	requireNoError(t, db.Close(ctx))

	db2, err := NewLittBlockDB(dir, 0)
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
		requireBytesEqual(t, tx.Transaction, gotTx.Transaction, "tx data")
	}
}

func TestHeightTrackingLostOnRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := NewLittBlockDB(dir, 0)
	requireNoError(t, err)
	requireNoError(t, db.WriteBlock(ctx, makeBlock(5, 1)))
	requireNoError(t, db.Flush(ctx))
	requireNoError(t, db.Close(ctx))

	db2, err := NewLittBlockDB(dir, 0)
	requireNoError(t, err)
	defer db2.Close(ctx)

	_, err = db2.GetLowestBlockHeight(ctx)
	if err != blockdb.ErrNoBlocks {
		t.Fatalf("expected ErrNoBlocks after reopen, got %v", err)
	}
	_, err = db2.GetHighestBlockHeight(ctx)
	if err != blockdb.ErrNoBlocks {
		t.Fatalf("expected ErrNoBlocks after reopen, got %v", err)
	}
}

func TestBlockWithNoTransactions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	block := makeBlock(42, 0)
	requireNoError(t, db.WriteBlock(ctx, block))

	got, ok, err := db.GetBlockByHeight(ctx, 42)
	requireNoError(t, err)
	requireTrue(t, ok, "expected block at height 42")
	requireBlockEqual(t, block, got)

	got, ok, err = db.GetBlockByHash(ctx, block.Hash)
	requireNoError(t, err)
	requireTrue(t, ok, "expected block by hash")
	requireBlockEqual(t, block, got)
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

func requireBlockEqual(t *testing.T, expected, actual *blockdb.BinaryBlock) {
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
