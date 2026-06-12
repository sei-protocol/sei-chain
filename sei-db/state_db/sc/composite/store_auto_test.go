package composite

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// This file tests the types.Auto write mode: effective-mode derivation
// from migration metadata, runtime transitions via SetWriteMode, and the
// transition-safety rules (only adjacent forward steps, no exits from an
// in-flight migration).

func autoConfig(batch int) config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.Auto
	cfg.KeysToMigratePerBlock = batch
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	return cfg
}

// openAutoStore opens (or reopens) a composite store at dir in Auto mode.
func openAutoStore(t *testing.T, dir string, batch int) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, autoConfig(batch))
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	return cs
}

// runBlocks applies and commits n workload blocks.
func runBlocks(t *testing.T, cs *CompositeCommitStore, workload *migrationWorkload, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(5, 5, 1, 2, 2)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
}

// runUntilAtMigrationVersion drives blocks until flatkv's migration
// version reaches version, failing after maxBlocks.
func runUntilAtMigrationVersion(
	t *testing.T,
	cs *CompositeCommitStore,
	workload *migrationWorkload,
	version uint64,
	maxBlocks int,
) {
	t.Helper()
	for i := 0; i < maxBlocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(0, 2, 1, 1, 1)))
		_, err := cs.Commit()
		require.NoError(t, err)
		done, err := migration.IsAtVersion(flatKVReaderFor(cs), version)
		require.NoError(t, err)
		if done {
			return
		}
	}
	t.Fatalf("migration did not reach version %d within %d blocks", version, maxBlocks)
}

// hasLatticeHash reports whether the last commit info contains the
// synthetic evm_lattice store entry.
func hasLatticeHash(cs *CompositeCommitStore) bool {
	for _, si := range cs.LastCommitInfo().StoreInfos {
		if si.Name == "evm_lattice" {
			return true
		}
	}
	return false
}

// TestComposite_Auto_FullLifecycle walks an Auto-configured store through
// the entire migration chain with SetWriteMode, asserting effective-mode
// derivation, transition gating, read transparency, and lattice-gate
// behavior at the MemiavlOnly -> MigrateEVM seam.
func TestComposite_Auto_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xA070)
	const batch = 25

	cs := openAutoStore(t, dir, batch)
	defer func() { _ = cs.Close() }()

	// Fresh store derives MemiavlOnly.
	require.Equal(t, types.MemiavlOnly, cs.currentWriteMode)
	require.NotNil(t, cs.memIAVL, "Auto must open memiavl")
	require.NotNil(t, cs.flatKV, "Auto must open flatkv")

	for i := 0; i < 10; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.False(t, hasLatticeHash(cs),
		"effective MemiavlOnly must not contribute evm_lattice to the AppHash")
	require.False(t, cs.latticeAppendLatched.Load())

	// Same-mode call is a no-op.
	require.NoError(t, cs.SetWriteMode(types.MemiavlOnly))
	require.Equal(t, types.MemiavlOnly, cs.currentWriteMode)

	// Start the EVM migration.
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.Equal(t, types.MigrateEVM, cs.currentWriteMode)

	// Pre-first-commit the boundary is NotStarted: the lattice gate must
	// stay closed so the AppHash matches the pre-transition value.
	require.False(t, hasLatticeHash(cs))

	// Mid-flight: exiting the migration is illegal in both directions.
	runBlocks(t, cs, workload, 1)
	require.True(t, hasLatticeHash(cs),
		"lattice must join the AppHash once the boundary advances")
	require.Error(t, cs.SetWriteMode(types.EVMMigrated),
		"completion flip must be rejected while the migration is in flight")
	require.Error(t, cs.SetWriteMode(types.MemiavlOnly),
		"backward transition must be rejected")
	require.Error(t, cs.SetWriteMode(types.MigrateAllButBank),
		"skipping ahead must be rejected")

	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 200)
	requireOracleMatches(t, cs, workload.snapshotOracle())

	// Completion flip is now legal; skipping straight to the next
	// migration is still not.
	require.Error(t, cs.SetWriteMode(types.MigrateAllButBank))
	require.NoError(t, cs.SetWriteMode(types.EVMMigrated))
	require.Equal(t, types.EVMMigrated, cs.currentWriteMode)
	runBlocks(t, cs, workload, 2)
	requireOracleMatches(t, cs, workload.snapshotOracle())

	// Walk the rest of the chain. The workload only writes bank/ and
	// evm/, so MigrateAllButBank has nothing to copy and completes on its
	// first block.
	require.NoError(t, cs.SetWriteMode(types.MigrateAllButBank))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version2_MigrateAllButBank, 200)
	require.NoError(t, cs.SetWriteMode(types.AllMigratedButBank))
	runBlocks(t, cs, workload, 2)
	requireOracleMatches(t, cs, workload.snapshotOracle())

	require.NoError(t, cs.SetWriteMode(types.MigrateBank))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version3_FlatKVOnly, 200)
	require.NoError(t, cs.SetWriteMode(types.FlatKVOnly))
	require.Equal(t, types.FlatKVOnly, cs.currentWriteMode)

	runBlocks(t, cs, workload, 2)
	requireOracleMatches(t, cs, workload.snapshotOracle())
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))

	// End of the chain: no further transitions exist.
	require.Error(t, cs.SetWriteMode(types.MemiavlOnly))
	require.NoError(t, cs.SetWriteMode(types.FlatKVOnly)) // no-op
}

func TestComposite_Auto_IllegalTransitionsFromFresh(t *testing.T) {
	cs := openAutoStore(t, t.TempDir(), 10)
	defer func() { _ = cs.Close() }()
	require.Equal(t, types.MemiavlOnly, cs.currentWriteMode)

	for _, target := range []types.WriteMode{
		types.EVMMigrated,        // skip
		types.MigrateAllButBank,  // skip
		types.AllMigratedButBank, // skip
		types.MigrateBank,        // skip
		types.FlatKVOnly,         // skip
		types.Auto,               // never a target
		types.TestOnlyDualWrite,  // never a target
		types.WriteMode("bogus"), // unknown
	} {
		require.Error(t, cs.SetWriteMode(target), "transition to %q must be rejected", target)
		require.Equal(t, types.MemiavlOnly, cs.currentWriteMode,
			"failed transition must leave the effective mode untouched")
	}
}

func TestComposite_SetWriteModeRequiresAutoConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.Error(t, cs.SetWriteMode(types.MigrateEVM),
		"SetWriteMode must be rejected when the configured mode is not Auto")
}

func TestComposite_SetWriteModeBeforeLoadVersion(t *testing.T) {
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), autoConfig(10))
	require.NoError(t, err)
	require.Error(t, cs.SetWriteMode(types.MigrateEVM))
}

func TestMemiavl_SetWriteModeUnsupported(t *testing.T) {
	cs := memiavl.NewCommitStore(t.TempDir(), config.DefaultStateCommitConfig().MemIAVLConfig)
	require.Error(t, cs.SetWriteMode(types.MigrateEVM))
}

// TestComposite_Auto_RestartResume verifies effective-mode derivation
// across restarts: an in-flight migration resumes in its migration mode,
// and a completed one comes back up in the following steady state even if
// the operator never issued the completion SetWriteMode.
func TestComposite_Auto_RestartResume(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xA071)
	const batch = 5

	cs := openAutoStore(t, dir, batch)
	for i := 0; i < 10; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	runBlocks(t, cs, workload, 3)
	done, err := migration.IsAtVersion(flatKVReaderFor(cs), migration.Version1_MigrateEVM)
	require.NoError(t, err)
	require.False(t, done, "test must restart while the migration is in flight; lower batch")
	require.NoError(t, cs.Close())

	// Reopen mid-migration: Auto must derive MigrateEVM and resume.
	cs = openAutoStore(t, dir, batch)
	require.Equal(t, types.MigrateEVM, cs.currentWriteMode)
	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 500)
	requireOracleMatches(t, cs, workload.snapshotOracle())
	// Still in MigrateEVM until restarted or explicitly flipped.
	require.Equal(t, types.MigrateEVM, cs.currentWriteMode)
	require.NoError(t, cs.Close())

	// Reopen after completion: derivation auto-advances to EVMMigrated.
	cs = openAutoStore(t, dir, batch)
	defer func() { _ = cs.Close() }()
	require.Equal(t, types.EVMMigrated, cs.currentWriteMode)
	runBlocks(t, cs, workload, 2)
	requireOracleMatches(t, cs, workload.snapshotOracle())
}

// TestComposite_Auto_ReadOnlyHandle verifies that a read-only handle
// opened from an Auto store derives its own effective mode from disk.
func TestComposite_Auto_ReadOnlyHandle(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xA072)
	const batch = 5

	cs := openAutoStore(t, dir, batch)
	defer func() { _ = cs.Close() }()
	for i := 0; i < 5; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	runBlocks(t, cs, workload, 2)

	roCommitter, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	ro, ok := roCommitter.(*CompositeCommitStore)
	require.True(t, ok)
	defer func() { _ = ro.Close() }()
	require.Equal(t, types.MigrateEVM, ro.currentWriteMode)
	requireOracleMatches(t, ro, workload.snapshotOracle())
}

func TestComposite_Auto_InitializeRejectsNonCanonicalStores(t *testing.T) {
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), autoConfig(10))
	require.NoError(t, err)
	require.Error(t, cs.Initialize([]string{"not-a-canonical-store"}),
		"Auto must enforce canonical store names since the mode may become mixed")
}

func TestComposite_Auto_CopyUnavailable(t *testing.T) {
	cs := openAutoStore(t, t.TempDir(), 10)
	defer func() { _ = cs.Close() }()
	require.Nil(t, cs.Copy(),
		"Copy is unavailable under Auto because flatkv is always open")
}
