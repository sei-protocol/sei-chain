package composite

import (
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// workloadOpts controls the per-block volume of the fuzz workload.
type workloadOpts struct {
	readsPerBlock   int
	updatesPerBlock int
	deletesPerBlock int
	newKeysPerBlock int
	blocks          int
	// iteratorReadsPerBlock and proofReadsPerBlock are best-effort:
	// they are only exercised on stores in profile.iterableStores /
	// profile.proofSupportingStores. Set to 0 to disable.
	iteratorReadsPerBlock int
	proofReadsPerBlock    int
	// startingBlock is the block number of the first block driven by
	// this call (1-based). Used so successive calls to
	// simulateBlocksOnComposite against the same composite can assert
	// monotonically-increasing versions.
	startingBlock int
}

// defaultWorkloadOpts returns a reasonable starting point for an N-block
// run. Most tests pass blocks=N and accept the defaults for the rest;
// individual fields can be overridden as needed.
func defaultWorkloadOpts(blocks int) workloadOpts {
	return workloadOpts{
		readsPerBlock:         50,
		updatesPerBlock:       50,
		deletesPerBlock:       10,
		newKeysPerBlock:       100,
		blocks:                blocks,
		iteratorReadsPerBlock: 3,
		proofReadsPerBlock:    3,
		startingBlock:         1,
	}
}

// blockOps captures the randomized changes scheduled for a single block,
// pre-applied to anything. Returned by generateBlockOps so the same
// schedule can be replayed against multiple CompositeCommitStores in
// lockstep (see TestCompositeFuzzStateSyncDuringMigration).
//
// addedKeys and removedKeys are the keyPair deltas that must be applied
// to the workload's liveKeySet once the changesets have been applied to
// every store the schedule is being replayed against.
type blockOps struct {
	changesets  []*proto.NamedChangeSet
	addedKeys   []keyPair
	removedKeys []keyPair
}

// generateBlockOps generates a single block of randomized work: a mix of
// inserts, updates, and deletes, distributed across profile.initialStores.
// The function does not mutate keysInUse so the caller decides exactly
// when to fold the deltas in (e.g. only after applying to every store in
// a lockstep run). Iteration over the per-store buckets is performed in
// sorted name order so the returned slice is byte-stable for a fixed
// rng seed.
func generateBlockOps(profile modeProfile, keysInUse *liveKeySet, rng *testutil.TestRandom, opts workloadOpts) blockOps {
	allPairs := make(map[string][]*proto.KVPair)
	var added []keyPair

	for i := 0; i < opts.newKeysPerBlock; i++ {
		store := profile.initialStores[rng.Intn(len(profile.initialStores))]
		var pair *proto.KVPair
		if store == keys.EVMStoreKey {
			pair = randomEVMKVPair(rng)
		} else {
			pair = randomKVPair(rng)
		}
		allPairs[store] = append(allPairs[store], pair)
		added = append(added, keyPair{store: store, key: string(pair.Key)})
	}

	for _, kp := range keysInUse.Sample(rng, opts.updatesPerBlock) {
		var value []byte
		if kp.store == keys.EVMStoreKey {
			value = randomEVMValue(rng, []byte(kp.key))
		} else {
			value = rng.Bytes(8)
		}
		allPairs[kp.store] = append(allPairs[kp.store],
			&proto.KVPair{Key: []byte(kp.key), Value: value})
	}

	removed := keysInUse.Sample(rng, opts.deletesPerBlock)
	for _, kp := range removed {
		allPairs[kp.store] = append(allPairs[kp.store],
			&proto.KVPair{Key: []byte(kp.key), Delete: true})
	}

	storeNames := make([]string, 0, len(allPairs))
	for store := range allPairs {
		storeNames = append(storeNames, store)
	}
	sort.Strings(storeNames)
	out := make([]*proto.NamedChangeSet, 0, len(allPairs))
	for _, store := range storeNames {
		out = append(out, &proto.NamedChangeSet{
			Name:      store,
			Changeset: proto.ChangeSet{Pairs: allPairs[store]},
		})
	}
	return blockOps{changesets: out, addedKeys: added, removedKeys: removed}
}

// applyBlockOpsTo applies ops's changesets to cs, commits, and returns
// the committed version. Caller is responsible for any per-block read
// sampling and for folding ops.addedKeys / ops.removedKeys into the
// workload's liveKeySet.
func applyBlockOpsTo(t *testing.T, cs *CompositeCommitStore, ops blockOps, expectedVersion int64) {
	t.Helper()
	require.NoError(t, cs.ApplyChangeSets(ops.changesets),
		"block %d: ApplyChangeSets", expectedVersion)
	version, err := cs.Commit()
	require.NoError(t, err, "block %d: Commit", expectedVersion)
	require.Equal(t, expectedVersion, version,
		"block %d: Commit must return expected version", expectedVersion)
	require.Equal(t, expectedVersion, cs.Version(),
		"block %d: cs.Version must agree with Commit's return", expectedVersion)
	lci := cs.LastCommitInfo()
	require.NotNil(t, lci, "block %d: LastCommitInfo must not be nil", expectedVersion)
	require.Equal(t, expectedVersion, lci.Version,
		"block %d: LastCommitInfo.Version must agree with cs.Version", expectedVersion)
}

// simulateBlocksOnComposite drives a randomized workload against cs and
// mirrors every write to oracle so the two stay in lockstep. After every
// block the helper:
//
//   - asserts cs.Commit() returned the expected (== block-number)
//     version and that cs.LastCommitInfo().Version agrees with cs.Version();
//   - samples reads through cs.Get / cs.Has and verifies they match the
//     oracle for the same keys;
//   - additionally samples Iterator / GetProof on stores in the per-mode
//     capability sets when the corresponding *PerBlock count is > 0.
//
// All randomness is sourced from rng, so the same seed produces the
// byte-identical apply / commit sequence across runs.
func simulateBlocksOnComposite(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *oracleStore,
	keysInUse *liveKeySet,
	profile modeProfile,
	rng *testutil.TestRandom,
	opts workloadOpts,
) {
	t.Helper()

	iterableList := setToSortedSlice(profile.iterableStores)
	proofList := setToSortedSlice(profile.proofSupportingStores)

	for b := 0; b < opts.blocks; b++ {
		blockNumber := int64(opts.startingBlock + b)
		ops := generateBlockOps(profile, keysInUse, rng, opts)

		applyBlockOpsTo(t, cs, ops, blockNumber)
		oracle.Apply(ops.changesets)
		for _, kp := range ops.addedKeys {
			keysInUse.Add(kp)
		}
		for _, kp := range ops.removedKeys {
			keysInUse.Remove(kp)
		}

		// 4) Per-block read sample via Get / Has. Verifies oracle
		// equivalence on a small set of live keys each block.
		for _, kp := range keysInUse.Sample(rng, opts.readsPerBlock) {
			expected, expectedOK := oracle.Get(kp.store, []byte(kp.key))
			gotVal, gotOK, err := cs.Get(kp.store, []byte(kp.key))
			require.NoError(t, err, "block %d: cs.Get store=%q", blockNumber, kp.store)
			require.Equal(t, expectedOK, gotOK,
				"block %d: cs.Get found mismatch on store=%q key=%x", blockNumber, kp.store, []byte(kp.key))
			require.Equal(t, expected, gotVal,
				"block %d: cs.Get value mismatch on store=%q key=%x", blockNumber, kp.store, []byte(kp.key))

			hasOK, err := cs.Has(kp.store, []byte(kp.key))
			require.NoError(t, err, "block %d: cs.Has store=%q", blockNumber, kp.store)
			require.Equal(t, gotOK, hasOK,
				"block %d: cs.Has must agree with cs.Get on store=%q key=%x",
				blockNumber, kp.store, []byte(kp.key))
		}

		// 5) Per-block Iterator sample on stores that route to a backend
		// that supports iteration (typically memiavl). The dbm.Iterator
		// contract demands non-nil start/end; we use the empty-then-end
		// pair to scan the whole tree.
		if opts.iteratorReadsPerBlock > 0 && len(iterableList) > 0 {
			for i := 0; i < opts.iteratorReadsPerBlock; i++ {
				store := iterableList[rng.Intn(len(iterableList))]
				iter, err := cs.Iterator(store, []byte{0x00}, []byte{0xFF}, true)
				require.NoError(t, err, "block %d: cs.Iterator store=%q", blockNumber, store)
				require.NotNil(t, iter, "block %d: cs.Iterator returned nil iterator for store=%q",
					blockNumber, store)
				// Walk a couple of entries to make sure the iterator
				// is functional; deeper iteration is exercised by the
				// end-of-test placement walk.
				for j := 0; j < 3 && iter.Valid(); j++ {
					iter.Next()
				}
				require.NoError(t, iter.Error(), "block %d: iterator error store=%q", blockNumber, store)
				require.NoError(t, iter.Close())
			}
		}

		// 6) Per-block GetProof sample. Only meaningful for memiavl-routed
		// stores; verifies the proof builder does not error and returns a
		// non-nil proof for keys that exist.
		if opts.proofReadsPerBlock > 0 && len(proofList) > 0 {
			for i := 0; i < opts.proofReadsPerBlock; i++ {
				// Pick a random live key in a proof-supporting store. If
				// the sampled key is not in such a store, skip this slot.
				sample := keysInUse.Sample(rng, 1)
				if len(sample) == 0 {
					continue
				}
				kp := sample[0]
				if !profile.proofSupportingStores[kp.store] {
					continue
				}
				proof, err := cs.GetProof(kp.store, []byte(kp.key))
				require.NoError(t, err,
					"block %d: cs.GetProof store=%q key=%x", blockNumber, kp.store, []byte(kp.key))
				require.NotNil(t, proof,
					"block %d: cs.GetProof returned nil proof for store=%q key=%x",
					blockNumber, kp.store, []byte(kp.key))
			}
		}
	}
}

// verifyReadsEqual asserts cs.Get and cs.Has agree with oracle for every
// live oracle key. Caller-owned cs and oracle must reflect the same
// committed state; the routine performs no commits of its own.
func verifyReadsEqual(t *testing.T, cs *CompositeCommitStore, oracle *oracleStore) {
	t.Helper()
	for storeName, storeMap := range oracle.stores {
		for k, expected := range storeMap {
			key := []byte(k)
			gotVal, gotOK, err := cs.Get(storeName, key)
			require.NoError(t, err, "cs.Get store=%q key=%x", storeName, key)
			require.True(t, gotOK,
				"cs.Get not found for store=%q key=%x (oracle has it)", storeName, key)
			require.Equal(t, expected, gotVal,
				"cs.Get value mismatch on store=%q key=%x", storeName, key)
			hasOK, err := cs.Has(storeName, key)
			require.NoError(t, err, "cs.Has store=%q key=%x", storeName, key)
			require.True(t, hasOK,
				"cs.Has not found for store=%q key=%x", storeName, key)
		}
	}
}

// flatKVPhysicalKeyCount returns the count of physical rows present in
// cs.flatKV across every data DB. Pending (uncommitted) writes are not
// included. Panics if cs.flatKV is nil; callers must guard.
//
// "Physical" means raw DB rows in flatkv's underlying pebbledbs. flatkv
// merges nonce + codeHash for the same address into one accountDB row,
// so this can underreport vs. a naive "count of logical keys" view.
// Callers compare against oracleFlatkvShapeFor (which models that
// merging) rather than against a flat logical count.
func flatKVPhysicalKeyCount(t *testing.T, cs *CompositeCommitStore) int64 {
	t.Helper()
	require.NotNil(t, cs.flatKV, "flatKVPhysicalKeyCount called on a mode with no flatkv backend")
	iter := cs.flatKV.RawGlobalIterator()
	defer func() { _ = iter.Close() }()
	var count int64
	for ok := iter.First(); ok; ok = iter.Next() {
		count++
	}
	require.NoError(t, iter.Error())
	return count
}

// memIAVLPhysicalKeyCount returns the total number of keys stored across
// every tree in cs.memIAVL. Panics if cs.memIAVL is nil; callers must
// guard.
func memIAVLPhysicalKeyCount(t *testing.T, cs *CompositeCommitStore) int64 {
	t.Helper()
	require.NotNil(t, cs.memIAVL, "memIAVLPhysicalKeyCount called on a mode with no memiavl backend")
	var total int64
	for _, namedTree := range cs.memIAVL.GetDB().Trees() {
		iter := namedTree.Iterator(nil, nil, true)
		for ; iter.Valid(); iter.Next() {
			total++
		}
		require.NoError(t, iter.Error(), "memiavl tree %q iterator error", namedTree.Name)
		_ = iter.Close()
	}
	return total
}

// deepInspectPlacement performs end-of-test verification of the nested
// memiavl + flatkv contents against the oracle, using profile.finalPlacement
// to decide which backend each oracle key should live in.
//
// Per-key invariants:
//
//   - For every oracle (store, key, value) where the placement is
//     backendMemiavl: the key must be present in cs.memIAVL with the
//     oracle's value and (when flatkv exists) absent from cs.flatKV.
//   - For backendFlatKV: present in cs.flatKV with the oracle's value and
//     (when memiavl exists) absent from cs.memIAVL.
//   - For backendDualWriteEVM (TestOnlyDualWrite EVM keys): present in
//     both backends with the oracle's value.
//
// Physical-count invariants (no phantom rows):
//
//   - cs.memIAVL physical key count == oracle logical keys assigned to
//     backendMemiavl plus oracle logical keys assigned to backendDualWriteEVM
//     (the latter is the mirror copy that dual-write keeps in memiavl).
//   - cs.flatKV physical row count == sum over flatkv-backed stores of the
//     per-DB row counts oracleFlatkvShapeFor models (account merging,
//     storage tuple uniqueness, code per-address, legacy per-key), plus
//     1 if MigrationVersionKey is on disk.
//
// For active-migration modes additionally asserts MigrationVersionKey is
// present on flatkv and MigrationBoundaryKey is absent — i.e. migration
// completed during the test.
func deepInspectPlacement(t *testing.T, cs *CompositeCommitStore, oracle *oracleStore, profile modeProfile) {
	t.Helper()

	memMem := int64(0)
	dualEVM := int64(0)

	for storeName, storeMap := range oracle.stores {
		placement, ok := profile.finalPlacement[storeName]
		require.True(t, ok,
			"deepInspectPlacement: oracle contains store %q with no entry in profile.finalPlacement",
			storeName)
		for k, expected := range storeMap {
			key := []byte(k)
			switch placement {
			case backendMemiavl:
				memMem++
				assertMemiavlHas(t, cs, storeName, key, expected)
				if profile.hasFlatKV {
					assertFlatkvAbsent(t, cs, storeName, key)
				}
			case backendFlatKV:
				assertFlatkvHas(t, cs, storeName, key, expected)
				if profile.hasMemiavl {
					assertMemiavlAbsent(t, cs, storeName, key)
				}
			case backendDualWriteEVM:
				dualEVM++
				assertMemiavlHas(t, cs, storeName, key, expected)
				assertFlatkvHas(t, cs, storeName, key, expected)
			default:
				t.Fatalf("deepInspectPlacement: unknown backendID %v for store %q", placement, storeName)
			}
		}
	}

	// Migration-completion / no-completion checks.
	hasVersionKey := false
	if profile.hasFlatKV {
		_, hasVersionKey = cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey))
		_, hasBoundary := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey))
		if profile.isActiveMigration {
			require.True(t, hasVersionKey,
				"%s: MigrationVersionKey must be present on flatkv after migration completes",
				profile.name)
			require.False(t, hasBoundary,
				"%s: MigrationBoundaryKey must be absent on flatkv after migration completes",
				profile.name)
		} else {
			// Steady-state-only routers never write migration metadata.
			// FlatKVOnly is one of those modes; so are MemiavlOnly,
			// EVMMigrated, AllMigratedButBank and TestOnlyDualWrite.
			// In our fresh-start fuzz tests there is no upstream
			// migration to inherit metadata from, so the version and
			// boundary keys must both be absent.
			require.False(t, hasVersionKey,
				"%s: MigrationVersionKey must be absent (steady-state router does not write it)",
				profile.name)
			require.False(t, hasBoundary,
				"%s: MigrationBoundaryKey must be absent (steady-state router does not write it)",
				profile.name)
		}
	}

	// Physical-count invariants.
	if profile.hasMemiavl {
		expected := memMem + dualEVM
		got := memIAVLPhysicalKeyCount(t, cs)
		require.Equal(t, expected, got,
			"%s: memiavl physical key count mismatch (expected oracle-derived %d, got %d)",
			profile.name, expected, got)
	}
	if profile.hasFlatKV {
		shape := oracleFlatkvShapeFor(oracle, func(store string) bool {
			placement := profile.finalPlacement[store]
			return placement == backendFlatKV || placement == backendDualWriteEVM
		})
		extra := int64(0)
		if hasVersionKey {
			extra = 1
		}
		expected := shape.total() + extra
		got := flatKVPhysicalKeyCount(t, cs)
		require.Equal(t, expected, got,
			"%s: flatkv physical row count mismatch (expected oracle-derived %d "+
				"[account=%d code=%d storage=%d legacy=%d +%d migration]; got %d)",
			profile.name, expected,
			shape.accountRows, shape.codeRows, shape.storageRows, shape.legacyRows,
			extra, got)
	}
}

// assertMemiavlHas verifies a key/value exists in cs.memIAVL.
func assertMemiavlHas(t *testing.T, cs *CompositeCommitStore, store string, key, expected []byte) {
	t.Helper()
	require.NotNil(t, cs.memIAVL, "assertMemiavlHas called with nil memiavl")
	child := cs.memIAVL.GetChildStoreByName(store)
	require.NotNil(t, child, "memiavl child store missing for %q", store)
	got := child.Get(key)
	require.NotNil(t, got, "memiavl missing key store=%q key=%x", store, key)
	require.Equal(t, expected, got, "memiavl value mismatch store=%q key=%x", store, key)
}

// assertMemiavlAbsent verifies a key is not present in cs.memIAVL.
func assertMemiavlAbsent(t *testing.T, cs *CompositeCommitStore, store string, key []byte) {
	t.Helper()
	require.NotNil(t, cs.memIAVL, "assertMemiavlAbsent called with nil memiavl")
	child := cs.memIAVL.GetChildStoreByName(store)
	if child == nil {
		// Tree might not exist on memiavl in this mode (e.g. a store
		// that was created on flatkv only). Treat as absent.
		return
	}
	got := child.Get(key)
	require.Nil(t, got, "memiavl unexpectedly has key store=%q key=%x", store, key)
}

// assertFlatkvHas verifies a key/value exists in cs.flatKV.
func assertFlatkvHas(t *testing.T, cs *CompositeCommitStore, store string, key, expected []byte) {
	t.Helper()
	require.NotNil(t, cs.flatKV, "assertFlatkvHas called with nil flatkv")
	got, ok := cs.flatKV.Get(store, key)
	require.True(t, ok, "flatkv missing key store=%q key=%x", store, key)
	require.Equal(t, expected, got, "flatkv value mismatch store=%q key=%x", store, key)
}

// assertFlatkvAbsent verifies a key is not present in cs.flatKV.
func assertFlatkvAbsent(t *testing.T, cs *CompositeCommitStore, store string, key []byte) {
	t.Helper()
	require.NotNil(t, cs.flatKV, "assertFlatkvAbsent called with nil flatkv")
	_, ok := cs.flatKV.Get(store, key)
	require.False(t, ok, "flatkv unexpectedly has key store=%q key=%x", store, key)
}

// setToSortedSlice returns a deterministic sorted slice of a string set's
// keys. Iteration order of Go maps is not stable, so the workload uses
// this helper to keep its rng consumption reproducible.
func setToSortedSlice(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// =============================================================================
// State-sync drain / replay helpers (composite-local copies, intentionally
// renamed to avoid colliding with the same-shape helpers defined in
// store_test.go).
// =============================================================================

// fuzzExportItem holds one item produced by a types.Exporter. Exactly one
// of moduleName / node is populated per item.
type fuzzExportItem struct {
	moduleName string
	node       *types.SnapshotNode
}

// fuzzDrainExporter collects every item the exporter yields in stream
// order, stopping at the first errorutils.ErrorExportDone. Any other
// error fails the test.
func fuzzDrainExporter(t *testing.T, exp types.Exporter) []fuzzExportItem {
	t.Helper()
	var items []fuzzExportItem
	for {
		raw, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone),
				"unexpected exporter error: %v", err)
			break
		}
		switch v := raw.(type) {
		case string:
			items = append(items, fuzzExportItem{moduleName: v})
		case *types.SnapshotNode:
			items = append(items, fuzzExportItem{node: v})
		default:
			t.Fatalf("unexpected exporter item type %T", raw)
		}
	}
	return items
}

// fuzzReplayImport feeds a drained exporter stream into imp in the same
// order.
func fuzzReplayImport(t *testing.T, imp types.Importer, items []fuzzExportItem) {
	t.Helper()
	for _, it := range items {
		if it.moduleName != "" {
			require.NoError(t, imp.AddModule(it.moduleName),
				"importer AddModule %q", it.moduleName)
		} else {
			imp.AddNode(it.node)
		}
	}
}
