package receipt_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/require"
)

// walked is one receipt an iterator yielded, flattened for comparison.
type walked struct {
	block  uint64
	txHash common.Hash
}

// drainReceipts walks store from startBlock and returns what it yielded, checking that each
// receipt's own block and tx hash agree with the iterator's.
func drainReceipts(t *testing.T, store receipt.ReceiptStore, startBlock uint64) []walked {
	t.Helper()
	it, err := store.IterateReceipts(startBlock)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var got []walked
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			return got
		}
		r, err := it.Receipt()
		require.NoError(t, err)
		require.Equal(t, it.BlockNumber(), r.BlockNumber)
		require.Equal(t, it.TxHash().Hex(), r.TxHashHex)
		got = append(got, walked{block: it.BlockNumber(), txHash: it.TxHash()})
	}
}

// expectedWalk is the walk of blocks, each holding txPerBlock receipts written by writeLitBlocks.
func expectedWalk(blocks []uint64, txPerBlock uint32) []walked {
	var want []walked
	for _, block := range blocks {
		for tx := uint32(0); tx < txPerBlock; tx++ {
			want = append(want, walked{block: block, txHash: litTxHash(block, tx)})
		}
	}
	return want
}

// writeLitBlocks writes txPerBlock receipts into each of blocks.
func writeLitBlocks(t *testing.T, store receipt.ReceiptStore, ctx sdk.Context, blocks []uint64, txPerBlock uint32) {
	t.Helper()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	for _, block := range blocks {
		records := make([]receipt.ReceiptRecord, 0, txPerBlock)
		for tx := uint32(0); tx < txPerBlock; tx++ {
			records = append(records, litReceipt(block, tx, addr))
		}
		writeLitBlock(t, store, ctx, block, records...)
	}
}

// writeEmptyLitBlock commits a block that produced no receipts, the shape every block without EVM
// transactions takes.
func writeEmptyLitBlock(t *testing.T, store receipt.ReceiptStore, ctx sdk.Context, block uint64) {
	t.Helper()
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), nil)) //nolint:gosec // small test heights
	// The write may be applied in the background; the head is how a reader learns that it landed.
	require.Eventually(t, func() bool {
		return store.LatestVersion() >= int64(block) //nolint:gosec // small test heights
	}, 5*time.Second, time.Millisecond)
}

func TestReceiptIteratorWalksEveryReceipt(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	blocks := []uint64{1, 2, 3}
	writeLitBlocks(t, store, ctx, blocks, 2)

	require.Equal(t, expectedWalk(blocks, 2), drainReceipts(t, store, 0))
}

func TestReceiptIteratorStartsAtStartBlock(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	writeLitBlocks(t, store, ctx, []uint64{1, 2, 3}, 2)

	require.Equal(t, expectedWalk([]uint64{2, 3}, 2), drainReceipts(t, store, 2))
}

func TestReceiptIteratorStartBlockWithoutReceipts(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	writeLitBlocks(t, store, ctx, []uint64{1}, 2)
	writeEmptyLitBlock(t, store, ctx, 2)
	writeLitBlocks(t, store, ctx, []uint64{3}, 2)

	// Block 2 produced no receipts, so it is recorded as an empty part, and the walk positions
	// there rather than starting over from the oldest block.
	require.Equal(t, expectedWalk([]uint64{3}, 2), drainReceipts(t, store, 2))
}

// A chain whose blocks are mostly empty still has a part key at every block, so a walk starting
// in the middle of the empty stretch positions there.
func TestReceiptIteratorStartsInAnEmptyStretch(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	writeLitBlocks(t, store, ctx, []uint64{1}, 2)
	for block := uint64(2); block < 50; block++ {
		writeEmptyLitBlock(t, store, ctx, block)
	}
	writeLitBlocks(t, store, ctx, []uint64{50}, 2)

	require.Equal(t, expectedWalk([]uint64{50}, 2), drainReceipts(t, store, 25))
}

func TestReceiptIteratorStartAboveHead(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	writeLitBlocks(t, store, ctx, []uint64{1, 2}, 2)

	require.Empty(t, drainReceipts(t, store, 99))
}

func TestReceiptIteratorStartsAtRetentionFloor(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	writeLitBlocks(t, store, ctx, []uint64{1, 2, 3}, 2)
	require.NoError(t, receipt.PruneLittIdx(store, 3))

	require.Equal(t, expectedWalk([]uint64{3}, 2), drainReceipts(t, store, 1))
}

func TestReceiptIteratorWalksEveryPartOfABlock(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	addr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	// Two writes for one block append two parts, the shape legacy receipt migration produces.
	writeLitBlock(t, store, ctx, 1, litReceipt(1, 0, addr))
	writeLitBlock(t, store, ctx, 1, litReceipt(1, 1, addr))

	require.Equal(t, expectedWalk([]uint64{1}, 2), drainReceipts(t, store, 0))
}

// A store restored above genesis has never recorded a retention floor, so the earliest receipt it
// holds is the only thing that can start a walk from below it.
func TestReceiptIteratorStartsAtOldestStoredBlock(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	blocks := []uint64{5000, 5001, 5002}
	writeLitBlocks(t, store, ctx, blocks, 2)
	require.Equal(t, int64(0), store.EarliestVersion(), "a store that was never pruned records no floor")

	require.Equal(t, expectedWalk(blocks, 2), drainReceipts(t, store, 0))
}

func TestReceiptIteratorStaysExhausted(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	writeLitBlocks(t, store, ctx, []uint64{1}, 1)

	it, err := store.IterateReceipts(0)
	require.NoError(t, err)
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
	}
	ok, err := it.Next()
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, it.Close())
	require.NoError(t, it.Close())
}

func TestReceiptIteratorUnsupportedByPebble(t *testing.T) {
	store, _, _ := setupReceiptStore(t)

	it, err := store.IterateReceipts(0)
	require.ErrorIs(t, err, receipt.ErrRangeQueryNotSupported)
	require.Nil(t, it)
}
