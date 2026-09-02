package receipt_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/require"
)

// TestOfflineGetRangeEmpty verifies that GetRange on a directory with no receipt store reports no data,
// rather than erroring.
func TestOfflineGetRangeEmpty(t *testing.T) {
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = t.TempDir()
	ok, lowest, highest, err := receipt.GetRange(cfg)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, lowest)
	require.Zero(t, highest)
}

// TestOfflineGetRange verifies that GetRange reports the lowest and highest block heights written to
// the store, without opening it.
func TestOfflineGetRange(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	for block := uint64(3); block <= 9; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.NoError(t, store.Close())

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	ok, lowest, highest, err := receipt.GetRange(cfg)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(3), lowest)
	require.Equal(t, uint64(9), highest)
}

// TestOfflinePruneAfter verifies that PruneAfter discards receipt bodies and tag-index entries above
// the target height, updates the store's latest-block metadata, and leaves everything at or below the
// target intact and readable after reopening.
func TestOfflinePruneAfter(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const count = 10
	const keepThrough = 6
	for block := uint64(1); block <= count; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.NoError(t, store.Close())

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	require.NoError(t, receipt.PruneAfter(cfg, keepThrough))

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.Equal(t, int64(keepThrough), store.LatestVersion())

	for block := uint64(1); block <= count; block++ {
		r, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		if block <= keepThrough {
			require.NoErrorf(t, err, "block %d should survive", block)
			require.Equal(t, block, r.BlockNumber)
		} else {
			require.ErrorIsf(t, err, receipt.ErrNotFound, "block %d should have been rolled back", block)
		}
	}

	logs, err := store.FilterLogs(ctx, 1, count, filters.FilterCriteria{Addresses: []common.Address{addr}}, nil)
	require.NoError(t, err)
	require.Len(t, logs, keepThrough, "tag index should only report surviving blocks")
}

// TestOfflinePruneAfterNoOp verifies that pruning to a height at or above the current head leaves the
// store unchanged.
func TestOfflinePruneAfterNoOp(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const count = 5
	for block := uint64(1); block <= count; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.NoError(t, store.Close())

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	require.NoError(t, receipt.PruneAfter(cfg, count+10))

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.Equal(t, int64(count), store.LatestVersion())
	for block := uint64(1); block <= count; block++ {
		r, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		require.NoError(t, err)
		require.Equal(t, block, r.BlockNumber)
	}
}

// TestOfflinePruneAfterRefusesBelowRetentionFloor verifies that PruneAfter refuses to roll back below
// the store's retention floor, and mutates nothing when it does.
func TestOfflinePruneAfterRefusesBelowRetentionFloor(t *testing.T) {
	dir := t.TempDir()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = dir
	cfg.ExternalPruning = true // drive PruneHistory directly, no jittered background pruner racing the test
	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const count = 10
	for block := uint64(1); block <= count; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}

	prunable, ok := store.(controller.PrunableStore)
	require.True(t, ok)
	require.NoError(t, prunable.PruneHistory(6)) // retention floor now at block 6
	require.NoError(t, store.Close())

	err = receipt.PruneAfter(cfg, 3) // below the floor
	require.ErrorContains(t, err, "retention floor")

	// The refusal must not have mutated anything: reopening should show the store exactly as it was.
	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.Equal(t, int64(count), store.LatestVersion())
	r, err := store.GetReceiptFromStore(ctx, litTxHash(count, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(count), r.BlockNumber)
}
