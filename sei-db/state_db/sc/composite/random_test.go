package composite

// Randomized, oracle-based scenario tests for CompositeCommitStore. Each test
// drives a controlled random workload (via the harness in
// random_test_framework_test.go) and verifies the store against an in-memory
// reference model, deep-inspecting both backends. There is one test per
// WriteMode: five steady-state modes and three migration modes.
//
// Run just these with:
//
//	go test ./sei-db/state_db/sc/composite/ -run Random -v

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Steady-state scenarios.
// =============================================================================

// runSteadyStateScenario exercises a single non-migrating write mode through
// the full lifecycle: random CRUD, rollback-to-checkpoint, restart, state-sync
// clone, and (for dual-backend modes) crash reconciliation. After every phase
// it runs the appropriate subset of deep verifications.
func runSteadyStateScenario(t *testing.T, mode types.WriteMode) {
	dir := t.TempDir()
	rng := testutil.NewTestRandom()
	cfg := randomTestConfig(t, rng, mode)
	placement := steadyStatePlacement(mode)
	hasMemIAVL := mode != types.FlatKVOnly
	hasFlatKV := mode != types.MemiavlOnly

	oracle := newStoreOracle()
	keysInUse := newLiveKeySet()

	cs := openComposite(t, dir, cfg)

	// --- Phase 1: random CRUD + full verification. ---
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 10, updatesPerBlock: 8, deletesPerBlock: 3, newKeysPerBlock: 20, conflictsPerBlock: 5, blocks: 20,
	})
	verifyOracle(t, cs, oracle)
	verifyIteration(t, cs, oracle, randomTestStores)
	verifyBoundedIteration(t, cs, oracle, rng, randomTestStores)
	verifyKeyPlacement(t, cs, oracle, placement)
	verifyKeyCounts(t, cs, oracle, placement)
	verifyFlatKVRows(t, cs, oracle, placement)
	assertFlatKVMapsExercised(t, oracle, placement)
	verifyMigrationMetadata(t, cs, false, false)
	verifyCommitInfo(t, cs, hasFlatKV)
	verifyProofRouting(t, cs, oracle, placement)
	if hasFlatKV {
		require.NoError(t, flatkv.VerifyLtHash(cs.flatKV),
			"steady-state flatkv must pass full-scan LtHash verification")
	}

	// --- Phase 2: rollback to a checkpoint, then restart at the rolled-back
	// height to confirm the rollback is durable. ---
	checkpoint := cs.Version()
	snap := oracle.snapshot()
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 5, updatesPerBlock: 5, deletesPerBlock: 2, newKeysPerBlock: 10, blocks: 8,
	})
	require.NoError(t, cs.Rollback(checkpoint))
	cs = restartComposite(t, cs, dir, cfg)
	require.Equal(t, checkpoint, cs.Version(), "store must report the rolled-back version after restart")
	verifyOracle(t, cs, snap)
	verifyIteration(t, cs, snap, randomTestStores)
	verifyBoundedIteration(t, cs, snap, rng, randomTestStores)
	verifyKeyPlacement(t, cs, snap, placement)
	verifyFlatKVRows(t, cs, snap, placement)

	// Resume committing on top of the restored state.
	oracle = snap.snapshot()
	keysInUse = liveKeySetFromOracle(oracle)
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 5, updatesPerBlock: 5, deletesPerBlock: 2, newKeysPerBlock: 10, blocks: 5,
	})
	verifyOracle(t, cs, oracle)

	// --- Phase 3: restart mid-run (WAL tail past the last backend snapshot). ---
	cs = restartComposite(t, cs, dir, cfg)
	verifyOracle(t, cs, oracle)
	verifyIteration(t, cs, oracle, randomTestStores)
	verifyBoundedIteration(t, cs, oracle, rng, randomTestStores)
	verifyKeyPlacement(t, cs, oracle, placement)
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 5, updatesPerBlock: 5, deletesPerBlock: 2, newKeysPerBlock: 10, blocks: 5,
	})
	verifyOracle(t, cs, oracle)

	// --- Phase 3b: read-only historical load at the Phase-2 checkpoint. The
	// read-only handle must reconstruct the checkpoint snapshot exactly while
	// the writable store (now many versions ahead) is left untouched. ---
	writableVersion := cs.Version()
	readHistoricalSnapshot(t, cs, checkpoint, snap, randomTestStores)
	require.Equal(t, writableVersion, cs.Version(),
		"writable store version must be unchanged by a historical read-only load")
	verifyOracle(t, cs, oracle)

	// --- Phase 4: state-sync clone via export/import into a fresh directory. ---
	cloneVersion := cs.Version()
	clone := stateSyncClone(t, cs, cloneVersion, cfg)
	verifyOracle(t, clone, oracle)
	verifyIteration(t, clone, oracle, randomTestStores)
	verifyBoundedIteration(t, clone, oracle, rng, randomTestStores)
	verifyKeyPlacement(t, clone, oracle, placement)
	verifyKeyCounts(t, clone, oracle, placement)
	verifyFlatKVRows(t, clone, oracle, placement)
	if hasFlatKV {
		require.NoError(t, flatkv.VerifyLtHash(clone.flatKV),
			"state-sync clone flatkv must pass full-scan LtHash verification")
	}

	// The clone must be live: keep committing on it and re-verify.
	cloneOracle := oracle.snapshot()
	cloneKeys := liveKeySetFromOracle(cloneOracle)
	cloneRng := testutil.NewTestRandom()
	simulateBlocks(t, clone, cloneOracle, cloneRng, cloneKeys, randomTestStores, simParams{
		readsPerBlock: 5, updatesPerBlock: 5, deletesPerBlock: 2, newKeysPerBlock: 10, conflictsPerBlock: 3, blocks: 5,
	})
	verifyOracle(t, clone, cloneOracle)
	verifyKeyPlacement(t, clone, cloneOracle, placement)

	// --- Phase 5: crash reconciliation (dual-backend modes only). ---
	if hasMemIAVL && hasFlatKV {
		reconcileVersion := cs.Version()
		reconcileSnap := oracle.snapshot()
		// Commit one more block so the backends are ahead of the snapshot,
		// then crash flatkv back to the snapshot height behind memiavl.
		simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
			readsPerBlock: 3, updatesPerBlock: 3, deletesPerBlock: 1, newKeysPerBlock: 5, blocks: 1,
		})
		require.NoError(t, cs.Close())
		rollbackFlatKVIndependently(t, dir, cfg, reconcileVersion)

		// Reopen: LoadVersion(0) must detect the divergence and reconcile
		// memiavl down to the flatkv version.
		cs = openComposite(t, dir, cfg)
		require.Equal(t, reconcileVersion, cs.Version(),
			"reconcileVersions must bring both backends to the lower (flatkv) version")
		verifyOracle(t, cs, reconcileSnap)
		verifyIteration(t, cs, reconcileSnap, randomTestStores)
		verifyBoundedIteration(t, cs, reconcileSnap, rng, randomTestStores)
		verifyKeyPlacement(t, cs, reconcileSnap, placement)
		verifyFlatKVRows(t, cs, reconcileSnap, placement)

		// Continue committing on the reconciled state.
		oracle = reconcileSnap.snapshot()
		keysInUse = liveKeySetFromOracle(oracle)
		simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
			readsPerBlock: 5, updatesPerBlock: 5, deletesPerBlock: 2, newKeysPerBlock: 10, blocks: 4,
		})
		verifyOracle(t, cs, oracle)
		verifyKeyPlacement(t, cs, oracle, placement)
	}
}

func TestRandomSteadyState_MemiavlOnly(t *testing.T) {
	runSteadyStateScenario(t, types.MemiavlOnly)
}

func TestRandomSteadyState_FlatKVOnly(t *testing.T) {
	runSteadyStateScenario(t, types.FlatKVOnly)
}

func TestRandomSteadyState_EVMMigrated(t *testing.T) {
	runSteadyStateScenario(t, types.EVMMigrated)
}

func TestRandomSteadyState_AllMigratedButBank(t *testing.T) {
	runSteadyStateScenario(t, types.AllMigratedButBank)
}

func TestRandomSteadyState_TestOnlyDualWrite(t *testing.T) {
	runSteadyStateScenario(t, types.TestOnlyDualWrite)
}

// =============================================================================
// Migration scenarios.
// =============================================================================

// migrationScenario parameterizes the predecessor -> migration -> successor
// lifecycle for one migration write mode.
type migrationScenario struct {
	predecessorMode types.WriteMode
	migrationMode   types.WriteMode
	successorMode   types.WriteMode
	targetVersion   uint64
	// migratingStores is the subset of randomTestStores that physically move
	// from memiavl to flatkv during this migration step.
	migratingStores []string
}

// simulateUntilMigrationComplete drives single-block workloads until the
// flatkv migration version reaches targetVersion, failing if it does not
// complete within maxBlocks.
func simulateUntilMigrationComplete(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	rng *testutil.TestRandom,
	keysInUse *liveKeySet,
	stores []string,
	p simParams,
	targetVersion uint64,
	maxBlocks int,
) {
	t.Helper()
	per := p
	per.blocks = 1
	for range maxBlocks {
		simulateBlocks(t, cs, oracle, rng, keysInUse, stores, per)
		done, err := migration.IsAtVersion(flatKVReaderFor(cs), targetVersion)
		require.NoError(t, err)
		if done {
			return
		}
	}
	t.Fatalf("migration to version %d did not complete within %d blocks", targetVersion, maxBlocks)
}

// runMigrationScenario seeds data in the predecessor schema, reopens in the
// migration mode and verifies behavior mid-flight (including across a restart),
// drives the migration to completion, then flips to the successor steady-state
// mode and re-verifies.
func runMigrationScenario(t *testing.T, sc migrationScenario) {
	dir := t.TempDir()
	rng := testutil.NewTestRandom()
	oracle := newStoreOracle()
	keysInUse := newLiveKeySet()

	// --- Phase 1: seed data in the predecessor steady-state schema. A large
	// seed volume gives the migrating store enough source keys to stay in
	// flight across all the mid-flight events below (each block only drains
	// KeysToMigratePerBlock keys). ---
	predCfg := randomTestConfig(t, rng, sc.predecessorMode)
	cs := openComposite(t, dir, predCfg)
	// Sentinel keys make every assertMigrationInFlight below deterministic
	// (independent of how many keys the random workload deals each store).
	seedMigrationSentinels(t, cs, oracle, sc.migratingStores)
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 10, updatesPerBlock: 5, deletesPerBlock: 2, newKeysPerBlock: 24, conflictsPerBlock: 4, blocks: 20,
	})
	verifyOracle(t, cs, oracle)
	verifyKeyPlacement(t, cs, oracle, steadyStatePlacement(sc.predecessorMode))
	verifyFlatKVRows(t, cs, oracle, steadyStatePlacement(sc.predecessorMode))

	// --- Phase 2: reopen in the migration mode with a small batch so the
	// migration stays in flight long enough to exercise the hybrid path. The
	// batch size is randomized (small) to vary how quickly the boundary
	// advances. ---
	migCfg := randomTestConfig(t, rng, sc.migrationMode)
	migBatch := 3 + rng.Intn(3) // {3,4,5}
	// The per-block rate is no longer a persisted config; mirror production
	// (BeginBlock re-applies the gov param after every restart) by having the
	// framework re-apply it on every store open for the rest of the scenario.
	testMigrationBatchSize = migBatch
	defer func() { testMigrationBatchSize = 0 }()
	t.Logf("migration scenario %s->%s keysToMigratePerBlock=%d",
		sc.migrationMode, sc.successorMode, migBatch)
	cs = restartComposite(t, cs, dir, migCfg)

	// Pre-migration reads must be transparent across the boundary.
	verifyOracle(t, cs, oracle)

	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 8, updatesPerBlock: 4, deletesPerBlock: 1, newKeysPerBlock: 6, conflictsPerBlock: 3, blocks: 5,
	})

	// Mid-flight deep verification: the migration must be genuinely in flight,
	// reads/iteration transparent across the split keyspace, boundary metadata
	// present while the version key is not yet written.
	done, err := migration.IsAtVersion(flatKVReaderFor(cs), sc.targetVersion)
	require.NoError(t, err)
	require.False(t, done, "migration should still be in flight at the mid checkpoint")
	assertMigrationInFlight(t, cs, oracle, sc.migratingStores...)
	verifyOracle(t, cs, oracle)
	verifyIteration(t, cs, oracle, randomTestStores)
	verifyBoundedIteration(t, cs, oracle, rng, randomTestStores)
	verifyMigrationMetadata(t, cs, false, true)

	// --- Restart mid-migration and re-verify the in-flight state survives. ---
	cs = restartComposite(t, cs, dir, migCfg)
	assertMigrationInFlight(t, cs, oracle, sc.migratingStores...)
	verifyOracle(t, cs, oracle)
	verifyIteration(t, cs, oracle, randomTestStores)
	verifyBoundedIteration(t, cs, oracle, rng, randomTestStores)
	verifyMigrationMetadata(t, cs, false, true)

	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 8, updatesPerBlock: 4, deletesPerBlock: 1, newKeysPerBlock: 4, blocks: 3,
	})

	// --- Mid-migration edge interleavings: rollback, crash-reconcile, and a
	// state-sync clone, each while the migration is still in flight. Each keeps
	// the migration in flight so the run-to-completion below still has work. ---
	cs, oracle, keysInUse = runMidMigrationInterleavings(t, cs, dir, migCfg, oracle, keysInUse, rng, sc)

	// --- Phase 3: run to completion. ---
	simulateUntilMigrationComplete(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 6, updatesPerBlock: 3, deletesPerBlock: 1, newKeysPerBlock: 2,
	}, sc.targetVersion, 400)

	succPlacement := steadyStatePlacement(sc.successorMode)
	verifyOracle(t, cs, oracle)
	verifyIteration(t, cs, oracle, randomTestStores)
	verifyBoundedIteration(t, cs, oracle, rng, randomTestStores)
	verifyKeyPlacement(t, cs, oracle, succPlacement)
	verifyFlatKVRows(t, cs, oracle, succPlacement)
	assertFlatKVMapsExercised(t, oracle, succPlacement)
	verifyMigrationMetadata(t, cs, true, false)
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV),
		"post-migration flatkv must pass full-scan LtHash verification")

	// --- Phase 4: flip to the successor steady-state mode and re-verify. ---
	succCfg := randomTestConfig(t, rng, sc.successorMode)
	cs = restartComposite(t, cs, dir, succCfg)
	verifyOracle(t, cs, oracle)
	verifyIteration(t, cs, oracle, randomTestStores)
	verifyBoundedIteration(t, cs, oracle, rng, randomTestStores)
	verifyKeyPlacement(t, cs, oracle, succPlacement)
	verifyFlatKVRows(t, cs, oracle, succPlacement)

	// New writes under the successor mode must continue to land correctly.
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 6, updatesPerBlock: 3, deletesPerBlock: 1, newKeysPerBlock: 4, blocks: 5,
	})
	verifyOracle(t, cs, oracle)
	verifyKeyPlacement(t, cs, oracle, succPlacement)
}

// runMidMigrationInterleavings drives three edge events against an in-flight
// migration -- rollback-to-checkpoint, crash reconciliation (flatkv left behind
// memiavl), and a state-sync clone -- verifying state against the oracle after
// each and confirming the migration is still in flight so the caller's
// run-to-completion still has work to do. It returns the (possibly reopened)
// store, the resumed oracle, and the rebuilt live-key set.
//
// Each event keeps block counts small; the abundant Phase-1 seed guarantees the
// migrating store still has enough un-drained source keys to stay in flight.
func runMidMigrationInterleavings(
	t *testing.T,
	cs *CompositeCommitStore,
	dir string,
	migCfg config.StateCommitConfig,
	oracle *storeOracle,
	keysInUse *liveKeySet,
	rng *testutil.TestRandom,
	sc migrationScenario,
) (*CompositeCommitStore, *storeOracle, *liveKeySet) {
	t.Helper()

	// --- Event A: rollback to a mid-flight checkpoint, restart, resume. ---
	checkpoint := cs.Version()
	snap := oracle.snapshot()
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 4, updatesPerBlock: 3, deletesPerBlock: 1, newKeysPerBlock: 3, blocks: 2,
	})
	require.NoError(t, cs.Rollback(checkpoint))
	cs = restartComposite(t, cs, dir, migCfg)
	require.Equal(t, checkpoint, cs.Version(), "rollback must return the store to the checkpoint version")
	assertMigrationInFlight(t, cs, snap, sc.migratingStores...)
	verifyOracle(t, cs, snap)
	verifyIteration(t, cs, snap, randomTestStores)
	verifyBoundedIteration(t, cs, snap, rng, randomTestStores)
	verifyMigrationMetadata(t, cs, false, true)
	oracle = snap.snapshot()
	keysInUse = liveKeySetFromOracle(oracle)
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 4, updatesPerBlock: 3, deletesPerBlock: 1, newKeysPerBlock: 3, blocks: 2,
	})

	// --- Event B: crash reconciliation -- leave flatkv one version behind
	// memiavl, reopen, and confirm both land at the lower version with a
	// coherent (still-in-flight) boundary. ---
	reconcileVersion := cs.Version()
	reconcileSnap := oracle.snapshot()
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 3, updatesPerBlock: 2, deletesPerBlock: 1, newKeysPerBlock: 3, blocks: 1,
	})
	require.NoError(t, cs.Close())
	rollbackFlatKVIndependently(t, dir, migCfg, reconcileVersion)
	cs = openComposite(t, dir, migCfg)
	require.Equal(t, reconcileVersion, cs.Version(),
		"reconcileVersions must bring both backends to the lower (flatkv) version mid-migration")
	assertMigrationInFlight(t, cs, reconcileSnap, sc.migratingStores...)
	verifyOracle(t, cs, reconcileSnap)
	verifyIteration(t, cs, reconcileSnap, randomTestStores)
	verifyBoundedIteration(t, cs, reconcileSnap, rng, randomTestStores)
	verifyMigrationMetadata(t, cs, false, true)
	oracle = reconcileSnap.snapshot()
	keysInUse = liveKeySetFromOracle(oracle)
	simulateBlocks(t, cs, oracle, rng, keysInUse, randomTestStores, simParams{
		readsPerBlock: 3, updatesPerBlock: 2, deletesPerBlock: 1, newKeysPerBlock: 3, blocks: 1,
	})

	// --- Event C: state-sync clone mid-migration. The boundary metadata lives
	// under the migration/ prefix in flatkv and rides along in the export, so
	// the clone (opened in the same migration mode) resumes the migration. We
	// verify the clone in flight, drive ITS migration to completion, and leave
	// the original store untouched and still in flight for the caller. ---
	cloneVersion := cs.Version()
	clone := stateSyncClone(t, cs, cloneVersion, migCfg)
	assertMigrationInFlight(t, clone, oracle, sc.migratingStores...)
	verifyOracle(t, clone, oracle)
	verifyIteration(t, clone, oracle, randomTestStores)
	verifyBoundedIteration(t, clone, oracle, rng, randomTestStores)
	verifyMigrationMetadata(t, clone, false, true)
	cloneOracle := oracle.snapshot()
	cloneKeys := liveKeySetFromOracle(cloneOracle)
	cloneRng := testutil.NewTestRandom()
	simulateUntilMigrationComplete(t, clone, cloneOracle, cloneRng, cloneKeys, randomTestStores, simParams{
		readsPerBlock: 4, updatesPerBlock: 2, deletesPerBlock: 1, newKeysPerBlock: 2,
	}, sc.targetVersion, 400)
	verifyOracle(t, clone, cloneOracle)
	verifyMigrationMetadata(t, clone, true, false)

	// The original store must remain in flight so the caller's completion runs.
	assertMigrationInFlight(t, cs, oracle, sc.migratingStores...)
	return cs, oracle, keysInUse
}

func TestRandomMigration_MigrateEVM(t *testing.T) {
	runMigrationScenario(t, migrationScenario{
		predecessorMode: types.MemiavlOnly,
		migrationMode:   types.MigrateEVM,
		successorMode:   types.EVMMigrated,
		targetVersion:   uint64(migration.Version1_MigrateEVM),
		migratingStores: []string{keys.EVMStoreKey},
	})
}

func TestRandomMigration_MigrateAllButBank(t *testing.T) {
	runMigrationScenario(t, migrationScenario{
		predecessorMode: types.EVMMigrated,
		migrationMode:   types.MigrateAllButBank,
		successorMode:   types.AllMigratedButBank,
		targetVersion:   uint64(migration.Version2_MigrateAllButBank),
		migratingStores: []string{keys.StakingStoreKey},
	})
}

func TestRandomMigration_MigrateBank(t *testing.T) {
	runMigrationScenario(t, migrationScenario{
		predecessorMode: types.AllMigratedButBank,
		migrationMode:   types.MigrateBank,
		successorMode:   types.FlatKVOnly,
		targetVersion:   uint64(migration.Version3_FlatKVOnly),
		migratingStores: []string{keys.BankStoreKey},
	})
}
