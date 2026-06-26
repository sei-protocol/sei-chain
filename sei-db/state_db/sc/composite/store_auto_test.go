package composite

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

func autoConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.Auto
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	return cfg
}

// openAutoStore opens (or reopens) a composite store at dir in Auto mode.
func openAutoStore(t *testing.T, dir string, batch int) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, autoConfig())
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(batch))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	return cs
}

// TestComposite_SetMigrationBatchSize_ClampsNegative pins the top-layer
// fallback: SetMigrationBatchSize is the single chokepoint feeding the router
// builders and the MigrationManager, so a negative (meaningless) rate is
// normalized to 0 (paused) here and the lower layers do no validation.
func TestComposite_SetMigrationBatchSize_ClampsNegative(t *testing.T) {
	dir := t.TempDir()
	cs := openAutoStore(t, dir, 0)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.SetMigrationBatchSize(-5))
	require.Equal(t, 0, cs.GetMigrationBatchSize(), "negative batch size must clamp to 0")

	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.Equal(t, 100, cs.GetMigrationBatchSize())

	require.NoError(t, cs.SetMigrationBatchSize(-1))
	require.Equal(t, 0, cs.GetMigrationBatchSize(), "negative batch size must re-clamp to 0")
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

// storeInfoNames returns the names in the last commit info, in order.
func storeInfoNames(cs *CompositeCommitStore) []string {
	infos := cs.LastCommitInfo().StoreInfos
	names := make([]string, 0, len(infos))
	for _, si := range infos {
		names = append(names, si.Name)
	}
	return names
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

	// Fresh store derives MemiavlOnly. flatkv is lazy: no instance and no
	// directory on disk until a MigrateEVM transition materializes it.
	require.Equal(t, types.MemiavlOnly, cs.currentWriteMode)
	require.NotNil(t, cs.memIAVL, "Auto must open memiavl")
	require.Nil(t, cs.flatKV, "Auto must not open flatkv before the first migration")
	require.NoDirExists(t, utils.GetFlatKVPath(dir),
		"Auto must not create the flatkv directory before the first migration")

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

	// Start the EVM migration. The transition materializes flatkv.
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.Equal(t, types.MigrateEVM, cs.currentWriteMode)
	require.NotNil(t, cs.flatKV, "MigrateEVM transition must materialize flatkv")
	require.DirExists(t, utils.GetFlatKVPath(dir))

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

	// Terminal state: the commit info must be shaped exactly like a
	// configured flatkv_only node's (no memiavl StoreInfos, only the
	// lattice) even though memiavl is still open under Auto.
	require.Equal(t, []string{"evm_lattice"}, storeInfoNames(cs),
		"effective FlatKVOnly must hash the same store set as configured flatkv_only")

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
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), autoConfig())
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

// autoExportConfig is autoConfig with per-block memiavl snapshots so the
// exporter can serve any committed version.
func autoExportConfig() config.StateCommitConfig {
	cfg := autoConfig()
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	return cfg
}

// openAutoStoreWithConfig mirrors openAutoStore for a caller-supplied config.
func openAutoStoreWithConfig(t *testing.T, dir string, cfg config.StateCommitConfig, batch int) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(batch))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	return cs
}

// moduleNamesOf extracts the module headers from an exported stream.
func moduleNamesOf(items []exportedItem) []string {
	var names []string
	for _, it := range items {
		if it.moduleName != "" {
			names = append(names, it.moduleName)
		}
	}
	return names
}

// TestComposite_Auto_ExportExcludesFlatKVUntilMigrationStarts pins the
// export side of the hash invariant at the MemiavlOnly -> MigrateEVM seam:
// a snapshot's sections must match the backends contributing to the
// AppHash at the exported version. In the materialize window (flatkv open
// and holding a seeded snapshot, boundary not yet advanced) the export
// must be byte-identical to a pre-transition export at the same height —
// otherwise nodes that have and have not transitioned advertise different
// snapshot streams for the same height.
func TestComposite_Auto_ExportExcludesFlatKVUntilMigrationStarts(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xA076)

	cs := openAutoStoreWithConfig(t, dir, autoExportConfig(), 100)
	defer func() { _ = cs.Close() }()
	runBlocks(t, cs, workload, 3)
	h := cs.Version()

	exp, err := cs.Exporter(h)
	require.NoError(t, err)
	preItems := drainCompositeExporter(t, exp)
	require.NoError(t, exp.Close())
	require.NotContains(t, moduleNamesOf(preItems), keys.FlatKVStoreKey)

	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.NotNil(t, cs.flatKV)

	exp, err = cs.Exporter(h)
	require.NoError(t, err)
	windowItems := drainCompositeExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Equal(t, preItems, windowItems,
		"export at the same height must not change when flatkv is materialized but not yet consensus-visible")

	// One block advances the boundary; flatkv joins both hash and export.
	runBlocks(t, cs, workload, 1)
	exp, err = cs.Exporter(cs.Version())
	require.NoError(t, err)
	postItems := drainCompositeExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Contains(t, moduleNamesOf(postItems), keys.FlatKVStoreKey)
}

// TestComposite_Auto_ExportImportRoundTrip pins the stream-driven import:
// a migrated Auto node's snapshot restored onto a FRESH Auto node (no
// flatkv directory) must materialize flatkv from the stream's section,
// after which derivation and reads work from the imported state.
func TestComposite_Auto_ExportImportRoundTrip(t *testing.T) {
	workload := newMigrationWorkload(0xA077)
	cfg := autoExportConfig()

	src := openAutoStoreWithConfig(t, t.TempDir(), cfg, 100)
	runBlocks(t, src, workload, 3)
	require.NoError(t, src.SetWriteMode(types.MigrateEVM))
	runUntilAtMigrationVersion(t, src, workload, migration.Version1_MigrateEVM, 200)
	require.NoError(t, src.SetWriteMode(types.EVMMigrated))
	runBlocks(t, src, workload, 1)
	h := src.Version()

	exp, err := src.Exporter(h)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Contains(t, moduleNamesOf(items), keys.FlatKVStoreKey)
	require.NoError(t, src.Close())

	// Fresh Auto destination: no flatkv directory, no flatkv instance.
	dstDir := t.TempDir()
	dst := openAutoStoreWithConfig(t, dstDir, cfg, 100)
	require.NoError(t, dst.Close())
	require.Nil(t, dst.flatKV)

	imp, err := dst.Importer(h)
	require.NoError(t, err)
	replayImport(t, imp, items)
	require.NoError(t, imp.Close())
	require.NotNil(t, dst.flatKV,
		"the stream's flatkv section must materialize the flatkv backend")
	require.DirExists(t, utils.GetFlatKVPath(dstDir))

	_, err = dst.LoadVersion(h, false)
	require.NoError(t, err)
	defer func() { _ = dst.Close() }()
	require.Equal(t, types.EVMMigrated, dst.currentWriteMode,
		"mode derivation must work from the imported migration metadata")
	requireOracleMatches(t, dst, workload.snapshotOracle())
}

// TestComposite_ImporterRejectsFlatKVSectionOnMemiavlOnly pins the loud
// rejection: silently dropping a flatkv section would restore a state
// tree missing data the snapshot's AppHash commits to.
func TestComposite_ImporterRejectsFlatKVSectionOnMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	imp, err := cs.Importer(1)
	require.NoError(t, err)
	require.NoError(t, imp.AddModule(keys.BankStoreKey))
	require.Error(t, imp.AddModule(keys.FlatKVStoreKey),
		"a flatkv section must be rejected loudly on a memiavl-only configuration")
	_ = imp.Close()
}

// TestComposite_Auto_MemiavlLeavesHashAtVersion3 pins the memiavl side of
// the commit-info gate: memiavl's StoreInfos drop out of the commit info
// at the commit that persists migration version 3 (bank migration
// complete) — while the effective mode is still MigrateBank, before any
// FlatKVOnly transition — and the exclusion is restart-independent (a
// reopened store derives FlatKVOnly and reports the identical commit
// info). Keying this on the mode instead of the persisted version would
// make a restarted node disagree with its live peers between completion
// and the transition trigger.
func TestComposite_Auto_MemiavlLeavesHashAtVersion3(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xA075)
	const batch = 25

	cs := openAutoStore(t, dir, batch)
	// Seed enough bank keys that the eventual bank migration takes
	// several blocks at this batch size, leaving observable mid-flight
	// commits.
	for i := 0; i < 10; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}

	// Walk the chain to MigrateBank.
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 200)
	require.NoError(t, cs.SetWriteMode(types.EVMMigrated))
	require.NoError(t, cs.SetWriteMode(types.MigrateAllButBank))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version2_MigrateAllButBank, 200)
	require.NoError(t, cs.SetWriteMode(types.AllMigratedButBank))
	require.NoError(t, cs.SetWriteMode(types.MigrateBank))

	// Drive to completion one block at a time: on every commit where
	// version 3 is not yet persisted, memiavl (which still carries
	// unmigrated bank state) must stay in the hash. The exclusion must
	// kick in exactly at the version-3 commit, while the mode is still
	// MigrateBank.
	sawMidFlight := false
	for i := 0; i < 200; i++ {
		done, err := migration.IsAtVersion(flatKVReaderFor(cs), migration.Version3_FlatKVOnly)
		require.NoError(t, err)
		if done {
			break
		}
		require.Contains(t, storeInfoNames(cs), keys.BankStoreKey,
			"memiavl StoreInfos must stay in the commit info while the bank migration is in flight")
		sawMidFlight = true
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(0, 2, 1, 1, 1)))
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.True(t, sawMidFlight,
		"test must observe at least one mid-flight MigrateBank commit; lower the batch size")
	require.Equal(t, types.MigrateBank, cs.currentWriteMode)
	require.Equal(t, []string{"evm_lattice"}, storeInfoNames(cs),
		"memiavl StoreInfos must leave the commit info at the version-3 commit, before the mode flips")
	preRestart := cs.LastCommitInfo()
	require.NoError(t, cs.Close())

	// Restart: derivation auto-advances to FlatKVOnly; the commit info
	// must be byte-identical to what the live MigrateBank node reported.
	cs = openAutoStore(t, dir, batch)
	defer func() { _ = cs.Close() }()
	require.Equal(t, types.FlatKVOnly, cs.currentWriteMode)
	require.Equal(t, preRestart, cs.LastCommitInfo(),
		"commit info must be restart-independent across the completion/transition window")
}

// TestComposite_Auto_InterruptedTransitionWindow simulates a crash after
// SetWriteMode(MigrateEVM) materialized flatkv but before any commit
// advanced the migration boundary. On reopen, derivation must come back as
// MemiavlOnly and the non-participating flatkv must be closed again
// (restoring the lazy-flatkv invariant); a re-fired transition must then
// succeed against the pre-existing directory.
func TestComposite_Auto_InterruptedTransitionWindow(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xA074)
	const batch = 5

	cs := openAutoStore(t, dir, batch)
	runBlocks(t, cs, workload, 3)
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.NotNil(t, cs.flatKV)
	// "Crash" before any post-transition commit: the boundary on flatkv
	// is still NotStarted.
	require.NoError(t, cs.Close())
	require.DirExists(t, utils.GetFlatKVPath(dir))

	// Reopen: the directory exists, but no migration has started, so the
	// effective mode reverts to MemiavlOnly and flatkv is closed again.
	cs = openAutoStore(t, dir, batch)
	defer func() { _ = cs.Close() }()
	require.Equal(t, types.MemiavlOnly, cs.currentWriteMode)
	require.Nil(t, cs.flatKV,
		"non-participating flatkv must be closed on reopen during the crash window")
	require.False(t, hasLatticeHash(cs),
		"lattice must stay out of the AppHash through the crash window")

	// Level-triggered re-fire: the transition succeeds against the
	// pre-existing directory and the migration then runs to completion.
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.NotNil(t, cs.flatKV)
	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 500)
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

// TestComposite_Auto_ReadOnlyPreFlatKVEraHeight pins the era-aware
// read-only path: heights that predate flatkv's history (the chain ran
// effectively memiavl-only) must remain queryable after the migration has
// begun. The handle skips flatkv entirely — at such heights all consensus
// data lives in memiavl — instead of failing the flatkv load. In-era
// heights keep loading flatkv.
func TestComposite_Auto_ReadOnlyPreFlatKVEraHeight(t *testing.T) {
	dir := t.TempDir()
	cs := openAutoStoreWithConfig(t, dir, autoExportConfig(), 100)
	defer func() { _ = cs.Close() }()

	// Heights 1..5 in the memiavl-only era, with a known per-height value.
	valAt := func(i int) []byte { return []byte{0x10 + byte(i)} }
	for i := 1; i <= 5; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: valAt(i)},
			}}},
		}))
		_, err := cs.Commit()
		require.NoError(t, err)
	}

	// Transition at height 5; drive a few blocks so the migration starts
	// and flatkv accumulates committed history.
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	for i := 0; i < 3; i++ {
		require.NoError(t, cs.ApplyChangeSets(nil))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(5), cs.flatKV.EarliestVersion(),
		"flatkv history must begin at the seeded (transition) height")

	// Pre-era height: served memiavl-only, with as-of-height values.
	roCommitter, err := cs.LoadVersion(3, true)
	require.NoError(t, err, "pre-flatkv-era heights must remain queryable")
	ro, ok := roCommitter.(*CompositeCommitStore)
	require.True(t, ok)
	require.Equal(t, types.MemiavlOnly, ro.currentWriteMode)
	require.Nil(t, ro.flatKV)
	val, found, err := ro.Get(keys.BankStoreKey, []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, valAt(3), val, "value as-of height 3")
	require.NoError(t, ro.Close())

	// In-era height: flatkv loads as before.
	roCommitter, err = cs.LoadVersion(7, true)
	require.NoError(t, err)
	ro, ok = roCommitter.(*CompositeCommitStore)
	require.True(t, ok)
	defer func() { _ = ro.Close() }()
	require.NotNil(t, ro.flatKV, "in-era heights must keep loading flatkv")
}

func TestComposite_Auto_InitializeRejectsNonCanonicalStores(t *testing.T) {
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), autoConfig())
	require.NoError(t, err)
	require.Error(t, cs.Initialize([]string{"not-a-canonical-store"}),
		"Auto must enforce canonical store names since the mode may become mixed")
}

// TestComposite_Auto_CopyAvailability pins Copy's behavior under Auto:
// available while flatkv has not been materialized (effective MemiavlOnly
// is indistinguishable from configured MemiavlOnly), unavailable once a
// migration opens flatkv.
func TestComposite_Auto_CopyAvailability(t *testing.T) {
	workload := newMigrationWorkload(0xA073)
	cs := openAutoStore(t, t.TempDir(), 10)
	defer func() { _ = cs.Close() }()

	runBlocks(t, cs, workload, 1)
	snap := cs.Copy()
	require.NotNil(t, snap,
		"Copy must work under Auto before flatkv is materialized")
	require.NoError(t, snap.Close())

	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.Nil(t, cs.Copy(),
		"Copy is unavailable once flatkv is open")
}
