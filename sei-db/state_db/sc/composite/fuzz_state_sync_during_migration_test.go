package composite

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
)

// TestCompositeFuzzStateSyncDuringMigration exercises the "snapshot taken
// mid-migration, restored into a second instance, both finish migrating
// from the same point" contract for every active-migration mode
// (MigrateEVM, MigrateAllButBank, MigrateBank).
//
// Per mode, the test:
//
//  1. Primes a source directory through every prior migration phase so it
//     ends up in the disk layout expected by the target mode (e.g. for
//     MigrateBank: MemiavlOnly → MigrateEVM (complete) → MigrateAllButBank
//     (complete)). The oracle is populated throughout so verification at
//     the end covers data written in every phase.
//
//  2. Opens the source in the target mode with a deliberately small
//     KeysToMigratePerBlock so the migration spans many blocks. Drives a
//     handful of blocks to be sure the boundary is mid-migration when the
//     snapshot is taken.
//
//  3. Takes an Exporter snapshot, drains it.
//
//  4. Opens a fresh destination directory in the target mode, replays the
//     snapshot through its Importer, and reloads the destination at the
//     source version.
//
//  5. Drives an identical workload — generated once from a shared rng — on
//     both src and dst block by block. After every block:
//
//     - both stores must agree on cs.Version() and cs.GetLatestVersion();
//     - both stores must agree with the oracle on Get / Has.
//
//     Continues until both stores' migrations complete (MigrationVersionKey
//     present in flatkv on both). A safety cap guards against infinite
//     loops if migration cannot converge.
//
//  6. End-of-test deep inspection on both src and dst.
//
// This is the strongest invariant the suite asserts about state sync:
// equality of the post-import migration behavior with the pre-export
// behavior, viewed through the migration boundary.
func TestCompositeFuzzStateSyncDuringMigration(t *testing.T) {
	const (
		// Priming controls (per phase).
		primingBlocksMemiavlOnly = 15
		// Prior-migration completion budget. With KeysToMigratePerBlock
		// at the default (1024) every prior migration drains in well
		// under this many blocks even after the heaviest priming.
		priorMigrationMaxBlocks = 40
		// Blocks driven on src in the target mode before snapshotting.
		// We want migration to be in-flight (boundary present), not yet
		// complete (version key absent).
		preSnapshotBlocks = 2
		// Safety cap for the parallel phase. The expected number of
		// blocks is bounded by (uncompleted-key-count / slow-rate)
		// plus the time needed for the migration manager to outpace
		// new "ahead-of-boundary" writes that show up each block; 200
		// is generous given the priming sizes and per-mode slow rates.
		parallelMaxBlocks = 200
	)

	for _, profile := range activeMigrationProfiles() {
		profile := profile
		t.Run(profile.name, func(t *testing.T) {
			sharedRng := testutil.NewTestRandom()
			srcDir := t.TempDir()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			// ---- Phase A: prime memiavl in MemiavlOnly mode so that
			// when we transition to the target migration mode,
			// memiavl has data to migrate and flatkv has not yet
			// been written to. flatkv's WAL therefore starts at the
			// version of the first MigrateEVM (or first prior phase)
			// commit, and the catchup performed by the read-only
			// exporter opens from a snapshot rather than from
			// version 1, so the "no WAL entries below the seed
			// version" condition does not surface as a WAL hole.
			//
			// withFlatKVSnapshotPerBlock is applied to every reopen
			// in this test: as soon as flatkv exists (Phase B and
			// later), the readonly Exporter catchup must be able to
			// load a snapshot whose metadata DB carries the seeded
			// version, otherwise catchup starts at v=0 and rejects
			// the seeded-but-no-prefix WAL.
			cs := newCompositeForMode(t, t.Context(), srcDir, lookupProfile("MemiavlOnly"),
				withFlatKVSnapshotPerBlock())
			simulateBlocksOnComposite(t, cs, oracle, keysInUse,
				lookupProfile("MemiavlOnly"), sharedRng,
				defaultWorkloadOpts(primingBlocksMemiavlOnly))
			require.NoError(t, cs.Close())

			// ---- Phase B: complete every prior migration mode in order ----
			for _, prior := range priorActiveModes(profile.writeMode) {
				priorProfile := lookupProfile(prior)
				cs := newCompositeForMode(t, t.Context(), srcDir, priorProfile,
					withFlatKVSnapshotPerBlock())

				opts := defaultWorkloadOpts(priorMigrationMaxBlocks)
				opts.startingBlock = int(cs.Version()) + 1
				simulateBlocksOnComposite(t, cs, oracle, keysInUse, priorProfile, sharedRng, opts)

				require.True(t, migrationCompleteFor(cs, priorProfile.writeMode),
					"%s: prior phase %s did not complete its migration in %d blocks",
					profile.name, prior, priorMigrationMaxBlocks)

				require.NoError(t, cs.Close())
			}

			// ---- Phase C: open target mode at a slow migration rate;
			// drive a few blocks to be sure migration is in-flight ----
			slowTarget := profile
			slowTarget.keysToMigratePerBlock = slowKeysPerBlockFor(profile.writeMode)

			src := newCompositeForMode(t, t.Context(), srcDir, slowTarget,
				withFlatKVSnapshotPerBlock())
			opts := defaultWorkloadOpts(preSnapshotBlocks)
			opts.startingBlock = int(src.Version()) + 1
			simulateBlocksOnComposite(t, src, oracle, keysInUse, slowTarget, sharedRng, opts)

			require.False(t, migrationCompleteFor(src, profile.writeMode),
				"%s: migration unexpectedly already complete after %d slow-rate blocks",
				profile.name, preSnapshotBlocks)
			require.True(t, migrationBoundaryPresent(src),
				"%s: MigrationBoundaryKey must be present mid-migration", profile.name)

			snapshotVersion := src.Version()

			// ---- Phase D: snapshot src, import into dst ----
			exporter, err := src.Exporter(snapshotVersion)
			require.NoError(t, err, "%s: src.Exporter", profile.name)
			items := fuzzDrainExporter(t, exporter)
			require.NoError(t, exporter.Close())

			// Close src then reopen so that any Exporter-internal state
			// is released before we start the parallel write phase.
			require.NoError(t, src.Close())
			src = newCompositeForMode(t, t.Context(), srcDir, slowTarget,
				withFlatKVSnapshotPerBlock())
			require.Equal(t, snapshotVersion, src.Version(),
				"%s: src.Version after reopen must equal snapshot version", profile.name)
			defer func() { _ = src.Close() }()

			dstDir := t.TempDir()
			dst := newCompositeForMode(t, t.Context(), dstDir, slowTarget,
				withFlatKVSnapshotPerBlock())
			require.NoError(t, dst.Close(), "%s: dst pre-import Close", profile.name)

			importer, err := dst.Importer(snapshotVersion)
			require.NoError(t, err, "%s: dst.Importer", profile.name)
			fuzzReplayImport(t, importer, items)
			require.NoError(t, importer.Close())

			_, err = dst.LoadVersion(snapshotVersion, false)
			require.NoError(t, err, "%s: dst.LoadVersion at imported version", profile.name)
			defer func() { _ = dst.Close() }()

			require.Equal(t, snapshotVersion, dst.Version(),
				"%s: dst.Version after import must equal snapshot version", profile.name)
			verifyReadsEqual(t, dst, oracle)

			require.False(t, migrationCompleteFor(dst, profile.writeMode),
				"%s: dst unexpectedly past migration completion immediately after import",
				profile.name)
			require.True(t, migrationBoundaryPresent(dst),
				"%s: dst MigrationBoundaryKey must be present after importing a mid-migration snapshot",
				profile.name)

			// ---- Phase E: drive identical workload on both until both
			// complete migration ----
			parallelOpts := defaultWorkloadOpts(1)
			// Keep the per-block read sample lightweight; deep checks
			// happen at the end.
			parallelOpts.readsPerBlock = 10
			parallelOpts.iteratorReadsPerBlock = 0
			parallelOpts.proofReadsPerBlock = 0

			var blocksDriven int
			for b := 0; b < parallelMaxBlocks; b++ {
				blocksDriven = b + 1
				ops := generateBlockOps(slowTarget, keysInUse, sharedRng, parallelOpts)
				expectedVersion := snapshotVersion + int64(blocksDriven)

				applyBlockOpsTo(t, src, ops, expectedVersion)
				applyBlockOpsTo(t, dst, ops, expectedVersion)

				oracle.Apply(ops.changesets)
				for _, kp := range ops.addedKeys {
					keysInUse.Add(kp)
				}
				for _, kp := range ops.removedKeys {
					keysInUse.Remove(kp)
				}

				require.Equal(t, src.Version(), dst.Version(),
					"%s parallel-block=%d: src and dst versions diverged",
					profile.name, blocksDriven)

				// Per-block sampled read parity against the oracle.
				for _, kp := range keysInUse.Sample(sharedRng, parallelOpts.readsPerBlock) {
					expected, expectedOK := oracle.Get(kp.store, []byte(kp.key))

					sv, sOK, err := src.Get(kp.store, []byte(kp.key))
					require.NoError(t, err,
						"%s parallel-block=%d: src.Get store=%q key=%x",
						profile.name, blocksDriven, kp.store, []byte(kp.key))
					require.Equal(t, expectedOK, sOK,
						"%s parallel-block=%d: src.Get found mismatch store=%q key=%x",
						profile.name, blocksDriven, kp.store, []byte(kp.key))
					require.Equal(t, expected, sv,
						"%s parallel-block=%d: src.Get value mismatch store=%q key=%x",
						profile.name, blocksDriven, kp.store, []byte(kp.key))

					dv, dOK, err := dst.Get(kp.store, []byte(kp.key))
					require.NoError(t, err,
						"%s parallel-block=%d: dst.Get store=%q key=%x",
						profile.name, blocksDriven, kp.store, []byte(kp.key))
					require.Equal(t, expectedOK, dOK,
						"%s parallel-block=%d: dst.Get found mismatch store=%q key=%x",
						profile.name, blocksDriven, kp.store, []byte(kp.key))
					require.Equal(t, expected, dv,
						"%s parallel-block=%d: dst.Get value mismatch store=%q key=%x",
						profile.name, blocksDriven, kp.store, []byte(kp.key))
				}

				if migrationCompleteFor(src, profile.writeMode) && migrationCompleteFor(dst, profile.writeMode) {
					break
				}
			}

			require.Less(t, blocksDriven, parallelMaxBlocks,
				"%s: migration failed to converge within parallelMaxBlocks=%d",
				profile.name, parallelMaxBlocks)
			require.True(t, migrationCompleteFor(src, profile.writeMode),
				"%s: src migration must complete in the parallel phase", profile.name)
			require.True(t, migrationCompleteFor(dst, profile.writeMode),
				"%s: dst migration must complete in the parallel phase", profile.name)

			// ---- Phase F: end-of-test verification ----
			verifyReadsEqual(t, src, oracle)
			verifyReadsEqual(t, dst, oracle)
			deepInspectPlacement(t, src, oracle, profile)
			deepInspectPlacement(t, dst, oracle, profile)
		})
	}
}

// lookupProfile returns the modeProfile whose Name field equals name.
// Panics on miss; callers pass literal mode names.
func lookupProfile(name string) modeProfile {
	for _, p := range allModeProfiles() {
		if p.name == name {
			return p
		}
	}
	panic("unknown mode profile name: " + name)
}

// slowKeysPerBlockFor returns a per-mode migration rate small enough to
// keep the target migration mid-flight after preSnapshotBlocks blocks,
// but large enough to converge in the parallel phase against the
// stochastic "new memiavl write rate" produced by the workload (which
// adds to the backlog whenever the new key is ahead of the boundary).
//
// Per-mode reasoning:
//
//   - MigrateEVM: only EVM data is in memiavl after Phase A (the
//     TestOnlyDualWrite priming). With newKeysPerBlock=100 spread across
//     ~20 modules, priming produces only ~75 EVM key writes, and the
//     finite EVM address pool then collapses those down to ~50–70 unique
//     logical keys. A rate of 10/block keeps the boundary in flight
//     across preSnapshotBlocks while still draining in <10 blocks during
//     the parallel phase.
//   - MigrateBank: bank is the only module in memiavl after the prior
//     migration phases complete. Over Phase A + 2 prior phases that is
//     hundreds of bank keys, so 25/block stays mid-flight at 2 blocks
//     and converges well within the parallel cap.
//   - MigrateAllButBank: 18 non-EVM-non-bank modules' worth of data
//     remains in memiavl. The slow rate must clear ~18× the new
//     ahead-of-boundary write rate per block, hence 100.
func slowKeysPerBlockFor(mode config.WriteMode) int {
	switch mode {
	case config.MigrateEVM:
		return 10
	case config.MigrateBank:
		return 25
	case config.MigrateAllButBank:
		return 100
	default:
		panic("slowKeysPerBlockFor: not an active-migration mode")
	}
}

// priorActiveModes returns the active-migration modes that must be
// completed before opening target. Order is the migration-version order
// the production migration ladder walks.
func priorActiveModes(target config.WriteMode) []string {
	switch target {
	case config.MigrateEVM:
		return nil
	case config.MigrateAllButBank:
		return []string{"MigrateEVM"}
	case config.MigrateBank:
		return []string{"MigrateEVM", "MigrateAllButBank"}
	default:
		panic("priorActiveModes: not an active-migration mode")
	}
}

// targetMigrationVersion returns the on-disk MigrationVersionKey value
// the migration manager writes when the given active-migration mode
// completes.
func targetMigrationVersion(mode config.WriteMode) uint64 {
	switch mode {
	case config.MigrateEVM:
		return migration.Version1_MigrateEVM
	case config.MigrateAllButBank:
		return migration.Version2_MigrateAllButBank
	case config.MigrateBank:
		return migration.Version3_FlatKVOnly
	default:
		panic("targetMigrationVersion: not an active-migration mode")
	}
}

// migrationCompleteFor reports whether cs.flatKV has
// MigrationVersionKey set to the target version of mode. Necessary —
// rather than a plain "version key present" check — because
// completing a prior migration also leaves a version key in flatkv
// (e.g. MigrateEVM writes value=1; subsequent MigrateAllButBank phase
// has its own target of 2).
func migrationCompleteFor(cs *CompositeCommitStore, mode config.WriteMode) bool {
	if cs.flatKV == nil {
		return false
	}
	raw, ok := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey))
	if !ok || len(raw) != 8 {
		return false
	}
	return binary.BigEndian.Uint64(raw) == targetMigrationVersion(mode)
}

// migrationBoundaryPresent reports whether MigrationBoundaryKey is
// currently persisted on cs.flatKV. The migration manager writes it on
// the first commit after start and removes it on the final block. Used
// to confirm a snapshot was actually taken mid-migration.
func migrationBoundaryPresent(cs *CompositeCommitStore) bool {
	if cs.flatKV == nil {
		return false
	}
	_, ok := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey))
	return ok
}
