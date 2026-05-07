package memblockdb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

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

func makeBlock(height uint64, numTxs int) block.Block {
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

func TestGetBlockHeightsEmpty(t *testing.T) {
	ctx := context.Background()
	db := NewMemBlockDB()

	_, err := db.GetLowestBlockHeight(ctx)
	if err == nil {
		t.Fatal("expected error on empty db, got nil")
	}
	_, err = db.GetHighestBlockHeight(ctx)
	if err == nil {
		t.Fatal("expected error on empty db, got nil")
	}
}

func TestGetBlockHeightsAfterWrites(t *testing.T) {
	ctx := context.Background()
	db := NewMemBlockDB()

	for _, h := range []uint64{5, 3, 8, 1, 10} {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(h, 1)))
	}

	lo, err := db.GetLowestBlockHeight(ctx)
	requireNoError(t, err)
	if lo != 1 {
		t.Fatalf("expected lowest height 1, got %d", lo)
	}

	hi, err := db.GetHighestBlockHeight(ctx)
	requireNoError(t, err)
	if hi != 10 {
		t.Fatalf("expected highest height 10, got %d", hi)
	}
}

func TestGetBlockHeightsAfterPrune(t *testing.T) {
	ctx := context.Background()
	db := NewMemBlockDB()

	for i := uint64(1); i <= 10; i++ {
		requireNoError(t, db.WriteBlock(ctx, makeBlock(i, 1)))
	}
	requireNoError(t, db.Prune(ctx, 6))

	lo, err := db.GetLowestBlockHeight(ctx)
	requireNoError(t, err)
	if lo != 6 {
		t.Fatalf("expected lowest height 6 after prune, got %d", lo)
	}

	hi, err := db.GetHighestBlockHeight(ctx)
	requireNoError(t, err)
	if hi != 10 {
		t.Fatalf("expected highest height 10 after prune, got %d", hi)
	}
}

func TestGetBlockHeightsSingleBlock(t *testing.T) {
	ctx := context.Background()
	db := NewMemBlockDB()

	requireNoError(t, db.WriteBlock(ctx, makeBlock(42, 0)))

	lo, err := db.GetLowestBlockHeight(ctx)
	requireNoError(t, err)
	if lo != 42 {
		t.Fatalf("expected lowest height 42, got %d", lo)
	}

	hi, err := db.GetHighestBlockHeight(ctx)
	requireNoError(t, err)
	if hi != 42 {
		t.Fatalf("expected highest height 42, got %d", hi)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
