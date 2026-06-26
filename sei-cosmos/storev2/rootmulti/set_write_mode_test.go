package rootmulti

import (
	"testing"

	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
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
	require.NoError(t, store.SetMigrationBatchSize(100))
	defer func() { require.NoError(t, store.Close()) }()

	// Deposit EVM state while effectively MemiavlOnly.
	addr := newEVMTestData(0xD1)
	for i := 1; i <= 2; i++ {
		simulateBlock(t, store, storeKeys, i, addr)
	}
	expected := makeSlot(2, 0xAA)

	// Capture views before the transition; rootmulti caches these in
	// ckvStores and SetWriteMode's rebuild will NOT refresh our local
	// references.
	staleEVM := store.GetKVStore(storeKeys["evm"])
	staleBank := store.GetKVStore(storeKeys["bank"])
	require.Equal(t, expected, staleEVM.Get(addr.storKey))

	require.NoError(t, store.SetWriteMode(sctypes.MigrateEVM))

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

// TestRootMultiSetWriteMode_RejectsPendingChanges pins the between-blocks
// contract enforcement: buffered writes that have not been flushed by
// Commit would be silently dropped by the view rebuild, so SetWriteMode
// must refuse to run.
func TestRootMultiSetWriteMode_RejectsPendingChanges(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, autoModeConfig())
	require.NoError(t, store.SetMigrationBatchSize(100))
	defer func() { require.NoError(t, store.Close()) }()

	addr := newEVMTestData(0xD2)
	simulateBlock(t, store, storeKeys, 1, addr)

	// Write directly into the cached view, bypassing the commit cycle:
	// the change is buffered in the commitment.Store and not yet flushed.
	store.GetKVStore(storeKeys["bank"]).Set([]byte("pending"), []byte{0x01})

	err := store.SetWriteMode(sctypes.MigrateEVM)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pending uncommitted changes")

	// After a normal commit cycle the same transition succeeds.
	cms := store.CacheMultiStore()
	cms.Write()
	_, err = store.GetWorkingHash()
	require.NoError(t, err)
	store.Commit(true)
	require.NoError(t, store.SetWriteMode(sctypes.MigrateEVM))
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
