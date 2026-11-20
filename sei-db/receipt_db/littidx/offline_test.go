package littidx_test

import (
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/littidx"
	"github.com/stretchr/testify/require"
)

// offlineCfg returns the config the offline entry points expect for a littidx store at dir. The backend
// must be set explicitly: the default is pebbledb, which these operations refuse.
func offlineCfg(dir string) dbconfig.ReceiptStoreConfig {
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	return cfg
}

// TestOfflineGetRangeEmpty verifies that GetRange on a directory with no receipt store reports no data,
// rather than erroring.
func TestOfflineGetRangeEmpty(t *testing.T) {
	cfg := offlineCfg(t.TempDir())
	ok, lowest, highest, err := littidx.GetRange(cfg)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, lowest)
	require.Zero(t, highest)
}

// TestOfflineGetLatestBlockOnAFreshStore verifies that GetLatestBlock reports no head for a directory
// that holds no store, and creates nothing while finding that out. Recovery reads the head on every
// startup, including a node's first, so it must not leave a half-made store behind.
func TestOfflineGetLatestBlockOnAFreshStore(t *testing.T) {
	dir := t.TempDir()
	cfg := offlineCfg(dir)

	block, err := littidx.GetLatestBlock(cfg)
	require.NoError(t, err)
	require.Zero(t, block)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "reading the head of a store that does not exist must not create one")
}

// TestOfflineGetLatestBlock verifies that GetLatestBlock reports the store's head without opening the
// store, both as written and after a rollback moves it.
func TestOfflineGetLatestBlock(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const highest = 7
	for block := uint64(1); block <= highest; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.NoError(t, store.Close())

	cfg := offlineCfg(dir)
	block, err := littidx.GetLatestBlock(cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(highest), block)

	const keepThrough = 4
	require.NoError(t, littidx.PruneAfter(cfg, keepThrough))
	block, err = littidx.GetLatestBlock(cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(keepThrough), block)
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

	cfg := offlineCfg(dir)
	ok, lowest, highest, err := littidx.GetRange(cfg)
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

	cfg := offlineCfg(dir)
	require.NoError(t, littidx.PruneAfter(cfg, keepThrough))

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.Equal(t, int64(keepThrough), store.LatestVersion())

	for block := uint64(1); block <= count; block++ {
		r, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		if block <= keepThrough {
			require.NoErrorf(t, err, "block %d should survive", block)
			require.Equal(t, block, r.BlockNumber)
		} else {
			require.ErrorIsf(t, err, receiptdb.ErrNotFound,
				"block %d should have been rolled back", block)
		}
	}

	logs, err := store.FilterLogs(ctx, 1, count, filters.FilterCriteria{Addresses: []common.Address{addr}}, nil)
	require.NoError(t, err)
	require.Len(t, logs, keepThrough, "tag index should only report surviving blocks")
}

// TestOfflinePruneAfterRemovesBlockStats verifies that a rollback also discards the block-stats entries
// above the target height, so a rolled-back block's stats don't linger for a height that no longer exists.
func TestOfflinePruneAfterRemovesBlockStats(t *testing.T) {
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

	cfg := offlineCfg(dir)
	require.NoError(t, littidx.PruneAfter(cfg, keepThrough))

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	for block := uint64(1); block <= count; block++ {
		stats, err := store.GetBlockStats(ctx, block)
		if block <= keepThrough {
			require.NoErrorf(t, err, "block %d's stats should survive", block)
			require.Equal(t, uint32(1), stats.TxCount)
		} else {
			require.ErrorIsf(t, err, receiptdb.ErrBlockStatsNotSupported,
				"block %d's stats should have been rolled back", block)
		}
	}
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

	cfg := offlineCfg(dir)
	require.NoError(t, littidx.PruneAfter(cfg, count+10))

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.Equal(t, int64(count), store.LatestVersion())
	for block := uint64(1); block <= count; block++ {
		r, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		require.NoError(t, err)
		require.Equal(t, block, r.BlockNumber)
	}
}

// TestOfflinePruneAfterBelowRetentionFloor verifies that PruneAfter below the retention floor lowers the
// floor to the target, so the receipts rewritten above it are served.
func TestOfflinePruneAfterBelowRetentionFloor(t *testing.T) {
	dir := t.TempDir()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)

	cfg := offlineCfg(dir)
	cfg.ExternalPruning = true // drive PruneHistory directly, no jittered background pruner racing the test
	store, err := littidx.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const count = 10
	for block := uint64(1); block <= count; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	prunable, ok := store.(controllertypes.PrunableStore)
	require.True(t, ok)
	require.NoError(t, prunable.PruneHistory(6)) // retention floor now at block 6
	require.NoError(t, store.Close())

	require.NoError(t, littidx.PruneAfter(cfg, 3)) // below the floor
	require.NoError(t, littidx.PruneAfter(cfg, 3), "a rerun must be a no-op")

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.Equal(t, int64(3), store.LatestVersion())
	require.Equal(t, int64(3), store.EarliestVersion(), "the floor must drop to the target")
	writeLitBlock(t, store, ctx, 4, litReceipt(4, 0, addr, topic))
	r, err := store.GetReceiptFromStore(ctx, litTxHash(4, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(4), r.BlockNumber)
}

// TestOfflinePruneAfterToZero verifies that PruneAfter to block 0 empties the store, retention floor
// included, that the emptied store takes block 1 again, and that a rerun is a no-op.
func TestOfflinePruneAfterToZero(t *testing.T) {
	dir := t.TempDir()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)

	cfg := offlineCfg(dir)
	cfg.ExternalPruning = true // drive PruneHistory directly, no jittered background pruner racing the test
	store, err := littidx.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const count = 10
	for block := uint64(1); block <= count; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	prunable, ok := store.(controllertypes.PrunableStore)
	require.True(t, ok)
	require.NoError(t, prunable.PruneHistory(6)) // retention floor now at block 6
	require.NoError(t, store.Close())

	require.NoError(t, littidx.PruneAfter(cfg, 0))
	require.NoError(t, littidx.PruneAfter(cfg, 0), "a rerun must be a no-op")

	head, err := littidx.GetLatestBlock(cfg)
	require.NoError(t, err)
	require.Zero(t, head)
	stored, _, _, err := littidx.GetRange(cfg)
	require.NoError(t, err)
	require.False(t, stored, "no receipt may survive a rollback to 0")

	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.Zero(t, store.LatestVersion())
	require.Zero(t, store.EarliestVersion(), "the floor must drop to 0")
	writeLitBlock(t, store, ctx, 1, litReceipt(1, 0, addr, topic))
	r, err := store.GetReceiptFromStore(ctx, litTxHash(1, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(1), r.BlockNumber)
}

// TestOfflinePruneAfterOnAFreshStore verifies that PruneAfter on a directory that holds no store creates
// nothing.
func TestOfflinePruneAfterOnAFreshStore(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, littidx.PruneAfter(offlineCfg(dir), 0))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "rolling back a store that does not exist must not create one")
}

// TestOfflinePruneAfterRefusesBelowOldestReceipt verifies that PruneAfter refuses a target below the
// oldest receipt on disk — the case an unset retention floor leaves uncovered — and mutates nothing.
func TestOfflinePruneAfterRefusesBelowOldestReceipt(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	const lowest = 3
	const highest = 9
	for block := uint64(lowest); block <= highest; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.NoError(t, store.Close())

	cfg := offlineCfg(dir)
	err := littidx.PruneAfter(cfg, lowest-1)
	require.ErrorContains(t, err, "no receipt would survive")

	// The refusal must leave the receipts in place rather than silently emptying the store while rewinding
	// the index — the failure this guard exists to prevent.
	store, ctx = setupLittIdx(t, dir)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.Equal(t, int64(highest), store.LatestVersion())
	for block := uint64(lowest); block <= highest; block++ {
		r, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		require.NoErrorf(t, err, "block %d should still be readable", block)
		require.Equal(t, block, r.BlockNumber)
	}

	logs, err := store.FilterLogs(ctx, 1, highest, filters.FilterCriteria{Addresses: []common.Address{addr}}, nil)
	require.NoError(t, err)
	require.Len(t, logs, highest-lowest+1, "tag index should be untouched")
}
