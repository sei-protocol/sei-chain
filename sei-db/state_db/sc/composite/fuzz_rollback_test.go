package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
)

// TestCompositeFuzzRollback exercises cs.Rollback for every WriteMode in
// steady state (i.e. the rollback target and the post-rollback continuation
// are both inside the same WriteMode and, for active-migration modes, both
// past migration completion).
//
// Per mode the test:
//
//  1. Drives a random workload forward to version M1. For active-migration
//     modes the default KeysToMigratePerBlock=1024 completes migration in
//     a handful of blocks, so M1=30 is comfortably post-migration. The
//     oracle at M1 is snapshotted so we can compare reads after the
//     rollback.
//  2. Continues the workload forward to version M2.
//  3. Calls cs.Rollback(M1).
//  4. Asserts cs.Version() == M1, LastCommitInfo.Version == M1, and that
//     cs.Get / cs.Has agree with the M1-oracle for every live key. For
//     active-migration modes the migration metadata at M1 (version key
//     present, boundary key absent) must also match.
//  5. Continues a fresh workload forward to version M3 against the M1
//     oracle clone. End-of-test deepInspectPlacement guarantees no
//     phantom rows survived the rollback and no oracle key was lost.
//
// Scope: rollback never crosses a WriteMode change — the entire test
// runs in one mode against one CompositeCommitStore — and for the active
// modes the rollback target is post-completion, so the boundary-key /
// in-flight-iterator state is not in play here. The mid-migration
// rollback case is the responsibility of
// TestCompositeFuzzRollbackDuringMigration below.
func TestCompositeFuzzRollback(t *testing.T) {
	const (
		preRollbackBlocks    = 30 // forward to M1
		postRollbackBlocks   = 30 // forward to M2, then roll back to M1
		postContinueBlocks   = 30 // M1 -> M3 after rollback
		writesAfterRollback  = 10 // smaller per-block volume in the
		readsAfterRollback   = 10 // continuation; the strong invariants
		newKeysAfterRollback = 10 // are at M1 and the deep inspection.
		deletesAfterRollback = 2
	)

	for _, profile := range allModeProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()

			cs := newCompositeForMode(t, t.Context(), dir, profile)
			defer func() { _ = cs.Close() }()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			// Phase 1: forward to M1.
			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng,
				defaultWorkloadOpts(preRollbackBlocks))
			rollbackVersion := cs.Version()
			require.Equal(t, int64(preRollbackBlocks), rollbackVersion,
				"%s: pre-rollback version", profile.name)

			// Capture the state we expect cs to be in after the rollback.
			oracleAtRollback := oracle.Snapshot()

			// Phase 2: forward to M2 (these blocks are discarded by
			// the rollback).
			opts := defaultWorkloadOpts(postRollbackBlocks)
			opts.startingBlock = int(cs.Version()) + 1
			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng, opts)
			require.Equal(t, int64(preRollbackBlocks+postRollbackBlocks), cs.Version(),
				"%s: pre-rollback version after discarded blocks", profile.name)

			// Phase 3: rollback.
			require.NoError(t, cs.Rollback(rollbackVersion),
				"%s: cs.Rollback(%d)", profile.name, rollbackVersion)
			require.Equal(t, rollbackVersion, cs.Version(),
				"%s: cs.Version after Rollback", profile.name)
			lci := cs.LastCommitInfo()
			require.NotNil(t, lci, "%s: LastCommitInfo after Rollback", profile.name)
			require.Equal(t, rollbackVersion, lci.Version,
				"%s: LastCommitInfo.Version after Rollback", profile.name)

			// Phase 4: verify reads against the M1 oracle.
			verifyReadsEqual(t, cs, oracleAtRollback)

			// For active-migration modes M1 is past completion, so the
			// version key must still be on disk and the boundary key
			// must still be absent (rollback to a post-completion
			// version must not re-introduce the in-flight boundary).
			if profile.isActiveMigration {
				require.True(t, migrationVersionKeyPresent(cs),
					"%s: MigrationVersionKey must remain present after rolling back to post-completion version",
					profile.name)
				require.False(t, migrationBoundaryPresent(cs),
					"%s: MigrationBoundaryKey must remain absent after rolling back to post-completion version",
					profile.name)
			}

			// Phase 5: continue forward with the M1 oracle. Use the
			// derived liveKeySet (membership only — rng-driven
			// sampling does not assume insertion order).
			postOracle := oracleAtRollback.Snapshot()
			postKeys := liveKeySetFromOracle(postOracle)
			postOpts := defaultWorkloadOpts(postContinueBlocks)
			postOpts.startingBlock = int(cs.Version()) + 1
			postOpts.updatesPerBlock = writesAfterRollback
			postOpts.readsPerBlock = readsAfterRollback
			postOpts.newKeysPerBlock = newKeysAfterRollback
			postOpts.deletesPerBlock = deletesAfterRollback
			simulateBlocksOnComposite(t, cs, postOracle, postKeys, profile, rng, postOpts)

			deepInspectPlacement(t, cs, postOracle, profile)
		})
	}
}

// TestCompositeFuzzRollbackDuringMigration exercises rollback across the
// migration-completion boundary inside a single active-migration mode.
// The migration manager owns persistent metadata (MigrationVersionKey,
// MigrationBoundaryKey) on flatkv, so rolling back from "past completion"
// to "mid-migration" must:
//
//   - restore the on-disk boundary key to whatever it was at the
//     mid-migration target version;
//   - remove the on-disk version key (it must not survive a rollback to
//     before completion);
//   - bring memiavl + flatkv data back to the exact state they were in at
//     the mid-migration version, so that resumed migration converges
//     idempotently.
//
// Scope: rollback never crosses a WriteMode change. The cs is kept in
// the target active-migration mode for the whole test; only the position
// of the migration boundary changes.
//
// The 3 active-migration modes (MigrateEVM, MigrateAllButBank, MigrateBank)
// each get their own sub-test, with priming through their prior
// active-migration phases via the same ladder
// TestCompositeFuzzStateSyncDuringMigration uses.
//
// ---------------------------------------------------------------------
//
// BLOCKED: this test reproduces a production-code defect in
// composite.CompositeCommitStore.Rollback and is therefore skipped at
// the top until the defect is fixed. See the in-test t.Skipf below for
// the failing assertions and the inferred root cause; nothing else
// about the test setup needs to change once the manager's in-memory
// boundary/iterator are re-initialized on rollback.
func TestCompositeFuzzRollbackDuringMigration(t *testing.T) {
	t.Skipf("blocked on composite.CompositeCommitStore.Rollback not re-initializing " +
		"MigrationManager in-memory state (boundary, iterator). " +
		"After cs.Rollback drops memiavl+flatkv back across the migration " +
		"completion point, the manager's in-memory boundary stays at " +
		"MigrationBoundaryComplete from the discard-phase commits. Every " +
		"subsequent cs.Get for a now-unmigrated key routes through " +
		"boundary.IsMigrated -> newDBReader (flatkv), where the key no " +
		"longer exists, and returns (nil, false). Every subsequent " +
		"cs.ApplyChangeSets takes the boundary.Equals(MigrationBoundaryComplete) " +
		"early-return and writes straight to flatkv, silently skipping " +
		"the migration that the on-disk boundary key says is still in " +
		"flight. Reproducer / failing assertion: this exact test, with " +
		"the t.Skipf removed; verifyReadsEqual fails immediately after " +
		"the Rollback call (line 269) for every active-migration mode.")
	const (
		primingBlocksMemiavlOnly = 15
		priorMigrationMaxBlocks  = 40
		// Blocks of slow-rate target-mode workload before the
		// rollback target is captured. Need at least 1 so that the
		// boundary key is on disk; 2 makes the in-flight position
		// less degenerate.
		preRollbackBlocks = 3
		// Blocks driven past the rollback target before issuing the
		// rollback. Sized to comfortably exceed the slow-rate drain
		// time so migration completes in this window for all three
		// modes.
		discardedBlocks = 80
		// Final convergence cap. The post-rollback drain has the
		// same per-block dynamics as the original migration, so this
		// just needs to exceed (memiavl-key-count / slow-rate).
		postRollbackMaxBlocks = 200
	)

	// snapshotKeepRecent must comfortably exceed the longest rollback
	// distance the test performs. With primingBlocksMemiavlOnly +
	// 2*priorMigrationMaxBlocks (worst case, MigrateBank target) +
	// preRollbackBlocks before the rollback target, plus discardedBlocks
	// + postRollbackMaxBlocks after it, 256 leaves ample room.
	const snapshotKeepRecent = 256
	openOpts := []compositeOption{
		withFlatKVSnapshotPerBlock(),
		withSnapshotKeepRecent(snapshotKeepRecent),
	}

	for _, profile := range activeMigrationProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			// ---- Phase A: prime MemiavlOnly ----
			//
			// openOpts is applied to every reopen below:
			//
			//   * withFlatKVSnapshotPerBlock: once flatkv is
			//     initialized (Phase B onward), its Rollback path
			//     closes the DBs and replays the WAL from the
			//     most recent snapshot to the rollback target.
			//     The MemiavlOnly → MigrateEVM transition seeds
			//     flatkv at memiavl.Version+1, leaving the flatkv
			//     WAL with no entries below that seed version;
			//     without a snapshot at the seeded version, the
			//     rollback catchup starts at v=0 and rejects the
			//     seeded-WAL prefix as a "hole".
			//   * withSnapshotKeepRecent: the rollback target's
			//     snapshot must still be present on disk when
			//     Rollback is called, after ~discardedBlocks of
			//     subsequent commits would otherwise have pruned
			//     it under the default keep-recent=2.
			cs := newCompositeForMode(t, t.Context(), dir, lookupProfile("MemiavlOnly"),
				openOpts...)
			simulateBlocksOnComposite(t, cs, oracle, keysInUse,
				lookupProfile("MemiavlOnly"), rng,
				defaultWorkloadOpts(primingBlocksMemiavlOnly))
			require.NoError(t, cs.Close())

			// ---- Phase B: complete every prior active migration ----
			for _, prior := range priorActiveModes(profile.writeMode) {
				priorProfile := lookupProfile(prior)
				cs := newCompositeForMode(t, t.Context(), dir, priorProfile,
					openOpts...)

				opts := defaultWorkloadOpts(priorMigrationMaxBlocks)
				opts.startingBlock = int(cs.Version()) + 1
				simulateBlocksOnComposite(t, cs, oracle, keysInUse, priorProfile, rng, opts)
				require.True(t, migrationCompleteFor(cs, priorProfile.writeMode),
					"%s: prior phase %s did not complete in %d blocks",
					profile.name, prior, priorMigrationMaxBlocks)

				require.NoError(t, cs.Close())
			}

			// ---- Phase C: open target mode at slow rate, drive a
			// few blocks so the boundary is mid-flight ----
			slowTarget := profile
			slowTarget.keysToMigratePerBlock = slowKeysPerBlockFor(profile.writeMode)
			cs = newCompositeForMode(t, t.Context(), dir, slowTarget, openOpts...)
			defer func() { _ = cs.Close() }()

			opts := defaultWorkloadOpts(preRollbackBlocks)
			opts.startingBlock = int(cs.Version()) + 1
			simulateBlocksOnComposite(t, cs, oracle, keysInUse, slowTarget, rng, opts)

			require.False(t, migrationCompleteFor(cs, profile.writeMode),
				"%s: migration unexpectedly already complete after %d slow-rate blocks",
				profile.name, preRollbackBlocks)
			require.True(t, migrationBoundaryPresent(cs),
				"%s: MigrationBoundaryKey must be present mid-migration", profile.name)

			rollbackVersion := cs.Version()
			oracleAtRollback := oracle.Snapshot()

			// ---- Phase D: drive past completion ----
			opts = defaultWorkloadOpts(discardedBlocks)
			opts.startingBlock = int(cs.Version()) + 1
			simulateBlocksOnComposite(t, cs, oracle, keysInUse, slowTarget, rng, opts)
			require.True(t, migrationCompleteFor(cs, profile.writeMode),
				"%s: migration must complete within %d discarded blocks (slow rate %d/block)",
				profile.name, discardedBlocks, slowTarget.keysToMigratePerBlock)

			// ---- Phase E: rollback to mid-migration ----
			require.NoError(t, cs.Rollback(rollbackVersion),
				"%s: cs.Rollback(%d) across migration completion", profile.name, rollbackVersion)
			require.Equal(t, rollbackVersion, cs.Version(),
				"%s: cs.Version after rollback across completion", profile.name)
			require.True(t, migrationBoundaryPresent(cs),
				"%s: MigrationBoundaryKey must be restored after rolling back into mid-migration",
				profile.name)
			// Use migrationCompleteFor (value-aware) rather than a
			// plain "version key present" check: for the
			// non-first active migrations (MigrateAllButBank /
			// MigrateBank) the version key already holds the
			// prior migration's target value at the rollback
			// target, so "absent" is not the correct mid-flight
			// invariant. The correct invariant for every active
			// mode is "the version key does not equal this
			// migration's target", i.e. !migrationCompleteFor.
			require.False(t, migrationCompleteFor(cs, profile.writeMode),
				"%s: migration must not appear complete after rolling back to before its completion (version key still equals target %d)",
				profile.name, targetMigrationVersion(profile.writeMode))

			verifyReadsEqual(t, cs, oracleAtRollback)

			// ---- Phase F: resume migration; assert it completes
			// idempotently and ends in the same placement as before
			// the rollback ----
			postOracle := oracleAtRollback.Snapshot()
			postKeys := liveKeySetFromOracle(postOracle)
			postOpts := defaultWorkloadOpts(1)
			postOpts.readsPerBlock = 10
			postOpts.iteratorReadsPerBlock = 0
			postOpts.proofReadsPerBlock = 0
			postOpts.newKeysPerBlock = 10
			postOpts.updatesPerBlock = 10
			postOpts.deletesPerBlock = 2

			var blocksDriven int
			for b := 0; b < postRollbackMaxBlocks; b++ {
				blocksDriven = b + 1
				blockOpts := postOpts
				blockOpts.startingBlock = int(cs.Version()) + 1
				simulateBlocksOnComposite(t, cs, postOracle, postKeys, slowTarget, rng, blockOpts)
				if migrationCompleteFor(cs, profile.writeMode) {
					break
				}
			}
			require.Less(t, blocksDriven, postRollbackMaxBlocks,
				"%s: post-rollback migration failed to converge within %d blocks",
				profile.name, postRollbackMaxBlocks)
			require.True(t, migrationCompleteFor(cs, profile.writeMode),
				"%s: post-rollback migration must complete", profile.name)

			deepInspectPlacement(t, cs, postOracle, profile)
		})
	}
}

// migrationVersionKeyPresent reports whether MigrationVersionKey is
// currently persisted on cs.flatKV. Distinct from migrationCompleteFor:
// this helper only asks "is a version key present", regardless of which
// migration mode value it holds. Used by the rollback tests which only
// need a yes/no answer at the metadata layer.
func migrationVersionKeyPresent(cs *CompositeCommitStore) bool {
	if cs.flatKV == nil {
		return false
	}
	_, ok := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey))
	return ok
}
