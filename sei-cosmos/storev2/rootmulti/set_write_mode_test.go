package rootmulti

import (
	"testing"

	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

func autoModeConfig() seidbconfig.StateCommitConfig {
	cfg := seidbconfig.DefaultStateCommitConfig()
	cfg.WriteMode = sctypes.Auto
	return withTestMemIAVL(cfg)
}

// TestRootMultiSetWriteMode_StaleViewsRouteCorrectly pins the live
// router-binding contract through the full rootmulti stack: a store view
// obtained BEFORE a runtime write-mode transition must serve correct reads
// AFTER the transition, even once the migration has moved its data from
// memiavl to flatkv. With a router captured by value at view construction
// the stale view would keep reading the emptied memiavl tree and return
// nil for migrated keys.
func TestRootMultiSetWriteMode_StaleViewsRouteCorrectly(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, autoModeConfig())
	defer func() { require.NoError(t, store.Close()) }()

	// Deposit EVM state while effectively MemiavlOnly (the batch size is
	// still 0, so the auto store stays in its pre-migration steady state).
	addr := newEVMTestData(0xD1)
	for i := 1; i <= 2; i++ {
		simulateBlock(t, store, storeKeys, i, addr)
	}
	expected := makeSlot(2, 0xAA)

	// Capture views before the transition; rootmulti caches these in
	// ckvStores and the kick-off's view rebuild will NOT refresh our local
	// references.
	staleEVM := store.GetKVStore(storeKeys["evm"])
	staleBank := store.GetKVStore(storeKeys["bank"])
	require.Equal(t, expected, staleEVM.Get(addr.storKey))

	// Raising the batch size above 0 is the migration trigger: it advances
	// the auto store memiavl_only -> migrate_evm.
	require.NoError(t, store.SetMigrationBatchSize(100))

	// Drive cosmos-only blocks: migration modes forward every flush, so
	// the batch=100 migration drains the handful of evm keys and deletes
	// them from memiavl, while the evm values themselves stay untouched.
	for i := 3; i <= 7; i++ {
		simulateCosmosOnlyBlock(t, store, storeKeys, i)
	}

	// The pre-transition view must agree with a fresh view and with the
	// last value written before the transition.
	freshEVM := store.GetKVStore(storeKeys["evm"])
	require.Equal(t, expected, freshEVM.Get(addr.storKey),
		"fresh view must read the migrated key")
	require.Equal(t, expected, staleEVM.Get(addr.storKey),
		"pre-transition view must route reads through the post-transition router")
	require.Equal(t, []byte{7}, staleBank.Get([]byte("supply")),
		"pre-transition bank view must keep serving current data")
}

// TestRootMultiSetWriteMode_RequiresAutoConfig confirms the error from the
// SC store propagates when the configured mode is fixed.
func TestRootMultiSetWriteMode_RequiresAutoConfig(t *testing.T) {
	dir := t.TempDir()
	store, _ := newTestRootMulti(t, dir, memiavlOnlyConfig())
	defer func() { require.NoError(t, store.Close()) }()

	err := store.SetWriteMode(sctypes.MigrateEVM)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fixed")
}

// TestRootMultiAutoKickoff_LiveCacheStoreNotOrphaned reproduces the production
// ordering: the block's deliver cache-multi-store is created at block start,
// BEFORE BeginBlock runs the kick-off. cacheMultiStore snapshots rs.ckvStores,
// so if the kick-off's SetWriteMode REPLACES those objects mid-block, writes
// made through the pre-created cms land in orphaned stores and are dropped at
// flush. This test pins that a write through that cms survives the commit.
func TestRootMultiAutoKickoff_LiveCacheStoreNotOrphaned(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, autoModeConfig())
	defer func() { require.NoError(t, store.Close()) }()

	addr := newEVMTestData(0xE4)
	storageKeys := storageMemIAVLKeys(0xE4, 6)
	simulateBlockManyStorage(t, store, storeKeys, 1, storageKeys, addr)

	// Deliver cms created at block start, before the kick-off.
	cms := store.CacheMultiStore()

	// BeginBlock kick-off: batch>0 flips memiavl_only -> migrate_evm.
	require.NoError(t, store.SetMigrationBatchSize(2))

	// DeliverTx-equivalent write through the pre-created cms.
	cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{0x42})
	cms.Write()

	_, err := store.GetWorkingHash()
	require.NoError(t, err)
	store.Commit(true)

	got := store.GetKVStore(storeKeys["bank"]).Get([]byte("supply"))
	require.Equal(t, []byte{0x42}, got,
		"write through the deliver cms created before the kick-off must not be lost")
}

// TestRootMultiAutoKickoff_FixedMemiavlOnlySkips proves the kick-off does not
// fire for a node pinned to fixed memiavl_only. Such a store reports the same
// memiavl_only effective mode as an auto store mid-pre-migration, but it must
// NOT be transitioned at runtime: SetMigrationBatchSize must return no error
// (it would otherwise hit SetWriteMode's "fixed by configuration" rejection)
// and must leave the mode at memiavl_only. The node deliberately opts out of
// the migration and diverges from auto peers from the activation height on.
func TestRootMultiAutoKickoff_FixedMemiavlOnlySkips(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, memiavlOnlyConfig())
	defer func() { require.NoError(t, store.Close()) }()

	addr := newEVMTestData(0xE3)
	simulateBlock(t, store, storeKeys, 1, addr)

	// A positive batch size must be a no-op for the fixed config (besides the
	// skip log): no error surfaces and the mode stays memiavl_only.
	require.NoError(t, store.SetMigrationBatchSize(100))
	mode, ok := store.GetWriteMode()
	require.True(t, ok)
	require.Equal(t, sctypes.MemiavlOnly, mode,
		"fixed memiavl_only must not be advanced by the kick-off")

	// And the node keeps committing in memiavl_only on subsequent blocks.
	simulateBlock(t, store, storeKeys, 2, addr)
	mode, _ = store.GetWriteMode()
	require.Equal(t, sctypes.MemiavlOnly, mode)
}

// TestRootMultiAutoKickoff_RestartResumesMigrateEVM proves the crash/restart
// safety of the batch-size migration kick-off. Once a positive batch size has
// advanced an auto store memiavl_only -> migrate_evm and the first migrating
// block has committed (persisting an in-flight boundary), a restart under the
// same auto config must DERIVE migrate_evm from that persisted boundary — the
// in-memory mode is not carried across the restart, so resumption depends
// entirely on the metadata. The node must resume mid-migration and, once the
// boundary is fully drained, a later restart must derive the evm_migrated
// steady state.
func TestRootMultiAutoKickoff_RestartResumesMigrateEVM(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, autoModeConfig())

	// Phase 1: deposit EVM storage keys while memiavl_only (batch size 0) so
	// the migration has real work and a single small-batch block leaves it
	// in-flight rather than completing immediately.
	addrBase := newEVMTestData(0xE1)
	storageKeys := storageMemIAVLKeys(0xE1, 12)
	simulateBlockManyStorage(t, store, storeKeys, 1, storageKeys, addrBase)

	mode, ok := store.GetWriteMode()
	require.True(t, ok)
	require.Equal(t, sctypes.MemiavlOnly, mode, "still memiavl_only before any batch size is set")

	// Kick off: batch > 0 advances memiavl_only -> migrate_evm.
	require.NoError(t, store.SetMigrationBatchSize(2))
	mode, _ = store.GetWriteMode()
	require.Equal(t, sctypes.MigrateEVM, mode, "batch>0 must kick off migrate_evm")

	// One migrating block persists the in-flight boundary at commit; batch=2
	// against 12 keys keeps the migration in flight (version not yet bumped).
	simulateBlock(t, store, storeKeys, 2, addrBase)

	// Restart under the SAME auto config (operator stop/start, or a crash
	// after this commit). The mode must be re-derived from persisted metadata.
	store, storeKeys = restartRootMultiWithConfig(t, store, dir, autoModeConfig())

	mode, ok = store.GetWriteMode()
	require.True(t, ok)
	require.Equal(t, sctypes.MigrateEVM, mode,
		"after restart the in-flight boundary must derive migrate_evm, not memiavl_only")

	// Resume draining to completion. The app re-applies the gov batch size
	// every BeginBlock; re-applying it here is a no-op for the mode (already
	// migrate_evm) and simply sets the rate.
	require.NoError(t, store.SetMigrationBatchSize(100))
	for i := 3; i <= 6; i++ {
		simulateBlock(t, store, storeKeys, i, addrBase)
	}
	require.NoError(t, store.Close())

	v, present := migrationVersionInFlatKV(t, dir, autoModeConfig())
	require.True(t, present)
	require.Equal(t, uint64(migration.Version1_MigrateEVM), v,
		"the migration must have completed (version bumped to 1)")

	// A restart after completion derives the evm_migrated steady state.
	store, _ = newTestRootMulti(t, dir, autoModeConfig())
	defer func() { require.NoError(t, store.Close()) }()
	mode, _ = store.GetWriteMode()
	require.Equal(t, sctypes.EVMMigrated, mode,
		"after completion a restart must derive evm_migrated")
}

// TestRootMultiAutoKickoff_RestartBeforeFirstCommitReKicks proves the
// level-triggered self-heal. If the node restarts after the kick-off flipped
// the mode in memory but before any migrating block committed (so no boundary
// is persisted), the restart correctly re-derives memiavl_only — and the next
// positive batch size (replayed every BeginBlock) re-fires the kick-off. This
// is the crash window SetWriteMode documents: the trigger is not consumed by a
// one-shot marker, so progress is neither lost nor duplicated.
func TestRootMultiAutoKickoff_RestartBeforeFirstCommitReKicks(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, autoModeConfig())

	addrBase := newEVMTestData(0xE2)
	storageKeys := storageMemIAVLKeys(0xE2, 8)
	simulateBlockManyStorage(t, store, storeKeys, 1, storageKeys, addrBase)

	// Kick off but DO NOT run a migrating block: the boundary is never
	// persisted (the flip lives only in memory; the freshly materialized
	// flatkv carries no in-flight boundary yet).
	require.NoError(t, store.SetMigrationBatchSize(2))
	mode, _ := store.GetWriteMode()
	require.Equal(t, sctypes.MigrateEVM, mode)

	// Restart: with no persisted in-flight boundary the derivation falls back
	// to memiavl_only (resolveCurrentWriteMode closes the idle flatkv).
	store, storeKeys = restartRootMultiWithConfig(t, store, dir, autoModeConfig())
	defer func() { require.NoError(t, store.Close()) }()
	mode, ok := store.GetWriteMode()
	require.True(t, ok)
	require.Equal(t, sctypes.MemiavlOnly, mode,
		"a kick-off not yet persisted must re-derive memiavl_only after restart")

	// The gov batch size, replayed by the app every BeginBlock, re-fires the
	// kick-off and the node converges to migrate_evm; the migration then runs
	// to completion normally.
	require.NoError(t, store.SetMigrationBatchSize(100))
	mode, _ = store.GetWriteMode()
	require.Equal(t, sctypes.MigrateEVM, mode, "re-applying batch>0 re-fires the kick-off")
	for i := 2; i <= 5; i++ {
		simulateBlock(t, store, storeKeys, i, addrBase)
	}
}
