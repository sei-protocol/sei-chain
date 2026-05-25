package composite

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// TestCompositeFuzzEdgeCases pins down a handful of adversarial
// change-set shapes that the random workloads in
// TestCompositeFuzzCRUDAllModes / TestCompositeFuzzStateSync* /
// TestCompositeFuzzRollback exercise too rarely to give them a chance
// to fail individually. Each sub-test is targeted at a single shape but
// runs across every mode where the shape is meaningful, so the same
// invariants (deep-inspection placement, oracle equivalence, version
// monotonicity, LastCommitInfo agreement) are checked everywhere.
//
// Scope:
//
//   - EmptyChangeSets: ApplyChangeSets(nil) + Commit must advance the
//     version, leave user-visible data unchanged, and (for active
//     migration modes) still let the migration manager progress its
//     internal iterator. Run on all 8 modes.
//   - SingleKeyBlocks: blocks containing exactly one KVPair, rotated
//     across initialStores so every store gets exercised at this volume.
//     Catches any code path that hard-codes a "multiple stores per
//     batch" assumption. Run on all 8 modes.
//   - AllDeleteBlocks: a single block deletes every key the oracle
//     knows about (after a small prime). For backendFlatKV / dual-write
//     EVM stores this also exercises flatkv's IsDelete pruning: the
//     end-of-test physical-row count must be zero in those DBs.
//     Run on all 8 modes.
//   - MigrationCompletesOnBlock1: opens each active-migration mode
//     against an empty (or empty-data) prior-state on-disk layout,
//     applies one empty block, and asserts the migration completes
//     in that first commit. Catches any code path that assumes
//     migration takes at least N>1 blocks to drain.
func TestCompositeFuzzEdgeCases(t *testing.T) {
	t.Run("EmptyChangeSets", testEdgeCaseEmptyChangeSets)
	t.Run("SingleKeyBlocks", testEdgeCaseSingleKeyBlocks)
	t.Run("AllDeleteBlocks", testEdgeCaseAllDeleteBlocks)
	t.Run("MigrationCompletesOnBlock1", testEdgeCaseMigrationCompletesOnBlock1)
}

// testEdgeCaseEmptyChangeSets primes each mode with a small random
// workload and then commits N consecutive empty blocks. Versions must
// keep advancing; oracle reads through cs.Get must keep matching; the
// per-block LastCommitInfo must still expose the right version. For
// active-migration modes the migration manager is still in the apply
// path and is free to write its own metadata / migrated batches even
// on a user-empty block — those writes are not visible through the
// oracle and the invariants above accommodate them.
func testEdgeCaseEmptyChangeSets(t *testing.T) {
	const (
		primeBlocks = 5
		emptyBlocks = 6
	)

	for _, profile := range allModeProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()

			cs := newCompositeForMode(t, t.Context(), dir, profile)
			defer func() { _ = cs.Close() }()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng,
				defaultWorkloadOpts(primeBlocks))

			versionBeforeEmpty := cs.Version()
			require.Equal(t, int64(primeBlocks), versionBeforeEmpty)

			for i := 0; i < emptyBlocks; i++ {
				expectedVersion := versionBeforeEmpty + int64(i+1)
				applyBlockOpsTo(t, cs, blockOps{}, expectedVersion)
				verifyReadsEqual(t, cs, oracle)
			}

			require.Equal(t, versionBeforeEmpty+int64(emptyBlocks), cs.Version(),
				"%s: empty blocks must advance the version", profile.name)

			deepInspectPlacement(t, cs, oracle, profile)
		})
	}
}

// testEdgeCaseSingleKeyBlocks commits N blocks, each containing exactly
// one KVPair, rotated across profile.initialStores. After every block
// the new key must read back through cs.Get / cs.Has. After all blocks
// deepInspectPlacement asserts every per-key placement is correct (no
// "first key in a block" or "single-store-per-block" code path leaked
// keys to the wrong backend).
//
// For active-migration modes the workload is too small to keep
// migration in-flight beyond a single block at the default rate, which
// is exactly the point: the migration manager must accept tiny user
// inputs and still produce a correct steady state.
func testEdgeCaseSingleKeyBlocks(t *testing.T) {
	const blocks = 32

	for _, profile := range allModeProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()

			cs := newCompositeForMode(t, t.Context(), dir, profile)
			defer func() { _ = cs.Close() }()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			storeRotation := append([]string(nil), profile.initialStores...)
			sort.Strings(storeRotation)

			for i := 0; i < blocks; i++ {
				store := storeRotation[i%len(storeRotation)]
				var pair *proto.KVPair
				if store == keys.EVMStoreKey {
					pair = randomEVMKVPair(rng)
				} else {
					pair = randomKVPair(rng)
				}
				ops := blockOps{
					changesets: []*proto.NamedChangeSet{
						{Name: store, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{pair}}},
					},
					addedKeys: []keyPair{{store: store, key: string(pair.Key)}},
				}
				expectedVersion := int64(i + 1)
				applyBlockOpsTo(t, cs, ops, expectedVersion)
				oracle.Apply(ops.changesets)
				for _, kp := range ops.addedKeys {
					keysInUse.Add(kp)
				}

				gotVal, gotOK, err := cs.Get(store, pair.Key)
				require.NoError(t, err,
					"%s block %d: cs.Get store=%q key=%x", profile.name, expectedVersion, store, pair.Key)
				require.True(t, gotOK,
					"%s block %d: cs.Get must find the just-written key store=%q key=%x",
					profile.name, expectedVersion, store, pair.Key)
				require.Equal(t, pair.Value, gotVal,
					"%s block %d: cs.Get value mismatch on store=%q key=%x",
					profile.name, expectedVersion, store, pair.Key)
				hasOK, err := cs.Has(store, pair.Key)
				require.NoError(t, err,
					"%s block %d: cs.Has store=%q key=%x", profile.name, expectedVersion, store, pair.Key)
				require.True(t, hasOK,
					"%s block %d: cs.Has must agree with cs.Get on the just-written key", profile.name, expectedVersion)
			}

			deepInspectPlacement(t, cs, oracle, profile)
		})
	}
}

// testEdgeCaseAllDeleteBlocks primes each mode with a few blocks of
// mixed-store data, then commits a single block whose changesets are
// nothing but deletes — one per key in the oracle. Post-block:
//
//   - every oracle key must read as not-present through cs.Get / cs.Has;
//   - the oracle is then cleared and the deep-inspection physical-count
//     invariants must hold against an empty oracle (i.e. the
//     memiavl/flatkv physical row counts are zero modulo any migration
//     metadata flatkv still holds).
//
// After that, a small random workload is appended to confirm cs still
// accepts writes / reads correctly post-mass-delete.
func testEdgeCaseAllDeleteBlocks(t *testing.T) {
	const (
		primeBlocks = 5
		afterBlocks = 3
	)

	for _, profile := range allModeProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()

			cs := newCompositeForMode(t, t.Context(), dir, profile)
			defer func() { _ = cs.Close() }()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng,
				defaultWorkloadOpts(primeBlocks))

			deleteOps := buildAllDeleteOps(oracle)
			deleteBlock := cs.Version() + 1
			applyBlockOpsTo(t, cs, deleteOps, deleteBlock)
			oracle.Apply(deleteOps.changesets)
			for _, kp := range deleteOps.removedKeys {
				keysInUse.Remove(kp)
			}

			for storeName, storeMap := range oracle.stores {
				require.Empty(t, storeMap,
					"%s: oracle must be empty after mass-delete (store=%q has %d keys remaining)",
					profile.name, storeName, len(storeMap))
			}
			require.Equal(t, 0, keysInUse.Len(),
				"%s: liveKeySet must be empty after mass-delete", profile.name)

			for _, kp := range deleteOps.removedKeys {
				gotVal, gotOK, err := cs.Get(kp.store, []byte(kp.key))
				require.NoError(t, err,
					"%s: cs.Get after mass-delete store=%q key=%x", profile.name, kp.store, []byte(kp.key))
				require.False(t, gotOK,
					"%s: cs.Get must report not-present after mass-delete store=%q key=%x",
					profile.name, kp.store, []byte(kp.key))
				require.Nil(t, gotVal,
					"%s: cs.Get value must be nil after mass-delete store=%q key=%x",
					profile.name, kp.store, []byte(kp.key))
				hasOK, err := cs.Has(kp.store, []byte(kp.key))
				require.NoError(t, err,
					"%s: cs.Has after mass-delete store=%q key=%x", profile.name, kp.store, []byte(kp.key))
				require.False(t, hasOK,
					"%s: cs.Has must report not-present after mass-delete store=%q key=%x",
					profile.name, kp.store, []byte(kp.key))
			}

			afterOpts := defaultWorkloadOpts(afterBlocks)
			afterOpts.startingBlock = int(cs.Version()) + 1
			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng, afterOpts)

			deepInspectPlacement(t, cs, oracle, profile)
		})
	}
}

// testEdgeCaseMigrationCompletesOnBlock1 sets up each active-migration
// mode so that, when its migration manager first runs, its source tree
// in memiavl is empty. Under that condition the manager's iterator
// returns boundary=Complete on the very first NextBatch call, the
// composite's first post-open commit writes MigrationVersionKey, and
// the mode behaves like its post-completion equivalent from block 2
// onward.
//
// The trick is that the composite's ApplyChangeSets short-circuits on
// an empty changeset (the router is never called), so completion only
// fires when a user write reaches the migration manager. Each prior
// mode is primed by a single write to that prior mode's
// migration-owned store, which:
//
//   - is sufficient to drive the prior manager's iterator exactly once
//     (it sees empty source -> boundary=Complete in that block);
//   - lands the prior's write directly in flatkv via the
//     "boundary is Complete, forward user writes to new DB" path; and
//   - leaves memiavl empty for every store, so the next mode's source
//     is empty when its migration runs.
//
// Catches any code path that silently assumes migration takes at least
// N>1 blocks to drain (off-by-one between the iterator's "Complete"
// signal and the version-key write, "first block is special" branches
// that only fire when there's data to move, etc.).
func testEdgeCaseMigrationCompletesOnBlock1(t *testing.T) {
	const afterBlocks = 5

	for _, profile := range activeMigrationProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()
			ctx := t.Context()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			primeThroughPriorActiveModesEmpty(t, ctx, dir, profile.writeMode,
				oracle, keysInUse, rng)

			cs := newCompositeForMode(t, ctx, dir, profile)
			defer func() { _ = cs.Close() }()

			versionBefore := cs.Version()

			triggerStore := migrationOwnedStoreFor(profile.writeMode)
			triggerPair := freshPairFor(triggerStore, rng)
			triggerOps := blockOps{
				changesets: []*proto.NamedChangeSet{
					{Name: triggerStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{triggerPair}}},
				},
				addedKeys: []keyPair{{store: triggerStore, key: string(triggerPair.Key)}},
			}
			expectedTriggerVersion := versionBefore + 1
			applyBlockOpsTo(t, cs, triggerOps, expectedTriggerVersion)
			oracle.Apply(triggerOps.changesets)
			for _, kp := range triggerOps.addedKeys {
				keysInUse.Add(kp)
			}

			require.True(t, migrationCompleteFor(cs, profile.writeMode),
				"%s: migration must complete on the first block after open when memiavl source is empty",
				profile.name)
			require.False(t, migrationBoundaryPresent(cs),
				"%s: MigrationBoundaryKey must be absent after immediate completion",
				profile.name)

			afterOpts := defaultWorkloadOpts(afterBlocks)
			afterOpts.startingBlock = int(cs.Version()) + 1
			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng, afterOpts)

			deepInspectPlacement(t, cs, oracle, profile)
		})
	}
}

// buildAllDeleteOps constructs a blockOps whose changesets delete every
// key currently in the oracle. The returned ops can be applied with
// applyBlockOpsTo and the caller is responsible for folding
// removedKeys back into its liveKeySet. Iteration over per-store maps
// and per-key strings is performed in sorted order so the produced
// changeset is byte-stable for a fixed oracle.
func buildAllDeleteOps(oracle *oracleStore) blockOps {
	storeNames := make([]string, 0, len(oracle.stores))
	for name, storeMap := range oracle.stores {
		if len(storeMap) == 0 {
			continue
		}
		storeNames = append(storeNames, name)
	}
	sort.Strings(storeNames)

	var ops blockOps
	for _, name := range storeNames {
		storeMap := oracle.stores[name]
		ks := make([]string, 0, len(storeMap))
		for k := range storeMap {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		pairs := make([]*proto.KVPair, 0, len(ks))
		for _, k := range ks {
			pairs = append(pairs, &proto.KVPair{Key: []byte(k), Delete: true})
			ops.removedKeys = append(ops.removedKeys, keyPair{store: name, key: k})
		}
		ops.changesets = append(ops.changesets, &proto.NamedChangeSet{
			Name:      name,
			Changeset: proto.ChangeSet{Pairs: pairs},
		})
	}
	return ops
}

// primeThroughPriorActiveModesEmpty walks dir through every prior
// active-migration mode of target, in order. For each prior mode it
// opens the mode and applies exactly one block that writes a single
// KVPair to the mode's migration-owned store. That:
//
//   - drives the prior manager's iterator exactly once. Because no
//     data was ever written to the prior's source tree, NextBatch
//     returns boundary=Complete on this first call and the same
//     commit writes the prior's MigrationVersionKey;
//   - the user's trigger write lands directly in flatkv via the
//     manager's "boundary is Complete, forward to new DB" path, so
//     memiavl stays empty for every store and the next prior mode
//     sees the same empty-source condition.
//
// Each trigger write is mirrored into oracle / keysInUse so the
// caller's end-of-test deepInspectPlacement covers the priming data
// too. Does nothing for MigrateEVM (priorActiveModes is empty).
func primeThroughPriorActiveModesEmpty(
	t *testing.T,
	ctx context.Context,
	dir string,
	target config.WriteMode,
	oracle *oracleStore,
	keysInUse *liveKeySet,
	rng *testutil.TestRandom,
) {
	t.Helper()
	for _, priorName := range priorActiveModes(target) {
		priorProfile := lookupProfile(priorName)
		cs := newCompositeForMode(t, ctx, dir, priorProfile)

		triggerStore := migrationOwnedStoreFor(priorProfile.writeMode)
		triggerPair := freshPairFor(triggerStore, rng)
		ops := blockOps{
			changesets: []*proto.NamedChangeSet{
				{Name: triggerStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{triggerPair}}},
			},
			addedKeys: []keyPair{{store: triggerStore, key: string(triggerPair.Key)}},
		}
		expectedVersion := cs.Version() + 1
		applyBlockOpsTo(t, cs, ops, expectedVersion)
		oracle.Apply(ops.changesets)
		for _, kp := range ops.addedKeys {
			keysInUse.Add(kp)
		}

		require.True(t, migrationCompleteFor(cs, priorProfile.writeMode),
			"prime: prior mode %q migration must complete on block 1 with empty memiavl source",
			priorName)

		require.NoError(t, cs.Close(), "prime: Close for prior mode %q", priorName)
	}
}

// migrationOwnedStoreFor returns a store name whose route in the given
// active-migration mode flows through the migration manager. A single
// non-empty write to this store drives the manager's iterator exactly
// once, which — combined with an empty source tree — is the minimal
// way to drive immediate migration completion through the composite
// layer (composite.ApplyChangeSets short-circuits empty changesets,
// and non-owned-store writes route via the passthrough router and
// never reach the manager).
func migrationOwnedStoreFor(mode config.WriteMode) string {
	switch mode {
	case config.MigrateEVM:
		return keys.EVMStoreKey
	case config.MigrateAllButBank:
		// Any store that's neither bank nor EVM works; staking is a
		// stable, always-present member of MemIAVLStoreKeys.
		return keys.StakingStoreKey
	case config.MigrateBank:
		return keys.BankStoreKey
	default:
		panic("migrationOwnedStoreFor: not an active-migration mode")
	}
}

// freshPairFor returns a single random KVPair shaped for store: EVM
// keys use the structured-EVM generator (so nonce / codehash / code /
// storage shapes are all reachable), every other store uses the plain
// 8-byte / 8-byte generator.
func freshPairFor(store string, rng *testutil.TestRandom) *proto.KVPair {
	if store == keys.EVMStoreKey {
		return randomEVMKVPair(rng)
	}
	return randomKVPair(rng)
}
