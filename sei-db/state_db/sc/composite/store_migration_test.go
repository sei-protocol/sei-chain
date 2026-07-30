package composite

import (
	"encoding/hex"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// This file contains composite-level integration tests for the
// FlatKV EVM migrate flow. The migration-package
// TestMigrateEVM (sei-db/state_db/sc/migration/migration_transitions_test.go)
// exercises the migration router directly against bare memiavl + flatkv
// CommitStores. The tests here move the same correctness assertions up
// one layer, so we also cover the composite's Initialize / LoadVersion /
// Commit lifecycle: migration-tree mounting on memiavl, the
// SetInitialVersion seeding that brings flatkv into lockstep on the
// MemiavlOnly -> MigrateEVM reopen, and the post-completion EVMMigrated
// flip operators perform once the boundary is gone.

// migKeyPair identifies a single (store, key) entry in the workload oracle.
type migKeyPair struct {
	store string
	key   string // stringified key bytes
}

// keySet is a deterministic-ordered set of byte-string keys. Adds and
// removes are O(1); Sample draws n distinct entries via Floyd's algorithm
// so its output depends only on the slice contents and the supplied RNG,
// not on Go's randomised map iteration order. This is the same approach
// used by the migration package's liveKeySet (see
// migration_test_framework_test.go) but kept local so the composite
// tests don't pull in any migration-package test-only helpers.
type keySet struct {
	keys []string
	idx  map[string]int
}

func newKeySet() *keySet { return &keySet{idx: map[string]int{}} }

func (s *keySet) len() int { return len(s.keys) }

func (s *keySet) add(k string) {
	if _, ok := s.idx[k]; ok {
		return
	}
	s.idx[k] = len(s.keys)
	s.keys = append(s.keys, k)
}

func (s *keySet) remove(k string) {
	i, ok := s.idx[k]
	if !ok {
		return
	}
	last := len(s.keys) - 1
	if i != last {
		s.keys[i] = s.keys[last]
		s.idx[s.keys[i]] = i
	}
	s.keys = s.keys[:last]
	delete(s.idx, k)
}

func (s *keySet) sample(rng *testutil.TestRandom, n int) []string {
	if n > len(s.keys) {
		n = len(s.keys)
	}
	if n == 0 {
		return nil
	}
	chosen := make(map[int]struct{}, n)
	out := make([]string, 0, n)
	for i := len(s.keys) - n; i < len(s.keys); i++ {
		j := rng.Intn(i + 1)
		if _, exists := chosen[j]; exists {
			chosen[i] = struct{}{}
			out = append(out, s.keys[i])
		} else {
			chosen[j] = struct{}{}
			out = append(out, s.keys[j])
		}
	}
	return out
}

// migrationWorkload generates a deterministic sequence of mixed
// EVM + bank changesets used to drive CompositeCommitStore through the
// MigrateEVM lifecycle. All randomness comes from a *testutil.TestRandom
// seeded by the caller, so two workloads constructed with the same seed
// and invoked with the same per-block parameters emit byte-identical
// changesets. That property is what
// TestComposite_MigrateEVM_DeterministicAcrossTwoStores relies on to
// assert per-block flatkv root-hash equality without any cross-run
// synchronisation.
//
// EVM keys are all storage-kind (0x03 prefix + 20-byte addr + 32-byte
// slot) so flatkv's key classifier routes them through the storage DB,
// which is the bulk of real EVM state and the hottest path for the
// migration's batch copier.
type migrationWorkload struct {
	rng      *testutil.TestRandom
	liveEVM  *keySet
	liveBank *keySet
	// expected mirrors the latest value written to every (store, key).
	// Maintained alongside the live key sets; deletes drop the entry.
	expected map[migKeyPair][]byte
}

func newMigrationWorkload(seed int64) *migrationWorkload {
	return &migrationWorkload{
		rng:      testutil.NewTestRandomNoPrint(seed),
		liveEVM:  newKeySet(),
		liveBank: newKeySet(),
		expected: map[migKeyPair][]byte{},
	}
}

// generateBlock produces a deterministic []*proto.NamedChangeSet
// representing one block of activity. Operation counts are interpreted
// as upper bounds; update/delete counts silently produce zero ops if
// the relevant live-set is empty, so the first block of a fresh
// workload may apply only new-key writes.
func (w *migrationWorkload) generateBlock(
	newEVMKeys, updateEVMKeys, deleteEVMKeys,
	newBankKeys, updateBankKeys int,
) []*proto.NamedChangeSet {
	var evmPairs, bankPairs []*proto.KVPair

	for i := 0; i < newEVMKeys; i++ {
		addr := w.rng.Bytes(keys.AddressLen)
		slot := w.rng.Bytes(32)
		stripped := append(addr, slot...)
		k := keys.BuildEVMKey(keys.EVMKeyStorage, stripped)
		v := w.rng.Bytes(32)
		evmPairs = append(evmPairs, &proto.KVPair{Key: k, Value: v})
		w.liveEVM.add(string(k))
		w.expected[migKeyPair{keys.EVMStoreKey, string(k)}] = append([]byte(nil), v...)
	}

	for _, k := range w.liveEVM.sample(w.rng, updateEVMKeys) {
		v := w.rng.Bytes(32)
		evmPairs = append(evmPairs, &proto.KVPair{Key: []byte(k), Value: v})
		w.expected[migKeyPair{keys.EVMStoreKey, k}] = append([]byte(nil), v...)
	}

	for _, k := range w.liveEVM.sample(w.rng, deleteEVMKeys) {
		evmPairs = append(evmPairs, &proto.KVPair{Key: []byte(k), Delete: true})
		w.liveEVM.remove(k)
		delete(w.expected, migKeyPair{keys.EVMStoreKey, k})
	}

	for i := 0; i < newBankKeys; i++ {
		k := append([]byte("b-"), w.rng.Bytes(16)...)
		v := w.rng.Bytes(16)
		bankPairs = append(bankPairs, &proto.KVPair{Key: k, Value: v})
		w.liveBank.add(string(k))
		w.expected[migKeyPair{keys.BankStoreKey, string(k)}] = append([]byte(nil), v...)
	}

	for _, k := range w.liveBank.sample(w.rng, updateBankKeys) {
		v := w.rng.Bytes(16)
		bankPairs = append(bankPairs, &proto.KVPair{Key: []byte(k), Value: v})
		w.expected[migKeyPair{keys.BankStoreKey, k}] = append([]byte(nil), v...)
	}

	// Emit changesets in fixed store-name order so the call sequence
	// handed to ApplyChangeSets is fully reproducible across runs.
	var out []*proto.NamedChangeSet
	if len(bankPairs) > 0 {
		out = append(out, &proto.NamedChangeSet{
			Name:      keys.BankStoreKey,
			Changeset: proto.ChangeSet{Pairs: bankPairs},
		})
	}
	if len(evmPairs) > 0 {
		out = append(out, &proto.NamedChangeSet{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: evmPairs},
		})
	}
	return out
}

// snapshotOracle returns a deep copy of the (store, key) -> value
// expectations so the caller can verify reads even after subsequent
// generateBlock calls have mutated the workload's internal state.
func (w *migrationWorkload) snapshotOracle() map[migKeyPair][]byte {
	out := make(map[migKeyPair][]byte, len(w.expected))
	for k, v := range w.expected {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// flatKVReaderFor builds a migration.DBReader pointing at the flatkv
// backend of the given composite store. Used to invoke
// migration.IsAtVersion from composite-package tests without having to
// reach into the migration package's private readVersionFromDB helper.
func flatKVReaderFor(cs *CompositeCommitStore) migration.DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		v, ok := cs.flatKV.Get(store, key)
		return v, ok, nil
	}
}

// driveMigrationWorkload runs the MemiavlOnly phase 1 + the MigrateEVM
// phase 2 in one open-close cycle, leaving the store closed on disk in
// MigrateEVM mode with a partially or fully drained boundary depending
// on the caller's batch size. Inside the reopen it asserts that phase-1
// reads still resolve through the migration router; the caller doesn't
// need to repeat that check.
//
// All three tests below need the same MemiavlOnly bootstrap followed
// by a reopen into MigrateEVM, so factoring it out keeps each test
// focused on what it asserts (deterministic hashes / resume / mode flip)
// rather than the boilerplate setup.
func driveMigrationWorkload(
	t *testing.T,
	dir string,
	workload *migrationWorkload,
	phase1Blocks, phase2Blocks int,
	keysToMigratePerBlock int,
) {
	t.Helper()

	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = types.MemiavlOnly
	// AsyncCommitBuffer=0 keeps WAL writes synchronous; without it
	// GetLatestVersion / on-disk reconcile races with the in-flight
	// commit and the post-reopen version checks become flaky.
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < phase1Blocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(phase1Blocks), cs.Version())
	require.Nil(t, cs.flatKV, "MemiavlOnly must not allocate flatkv")
	// Snapshot the oracle right at the mode boundary so the
	// post-reopen verification below sees pre-migration data only.
	// Phase 2 will then mutate the workload further; callers that
	// need the post-phase-2 oracle can re-snapshot via workload.
	preFlipOracle := workload.snapshotOracle()
	require.NoError(t, cs.Close())

	migCfg := config.DefaultStateCommitConfig()
	migCfg.WriteMode = types.MigrateEVM
	migCfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err = NewCompositeCommitStore(t.Context(), dir, migCfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(keysToMigratePerBlock))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Phase 1 reads must still resolve through the migration router.
	// Failing here means the read-transparency invariant (I3) is broken:
	// EVM lookups silently disappear during a migration boundary.
	requireOracleMatches(t, cs, preFlipOracle)

	for i := 0; i < phase2Blocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(5, 5, 1, 2, 2)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())
}

// reopenInMigrateEVM is a small helper for the resume / migration paths
// that need to peek at on-disk state from a MigrateEVM mode reopen.
func reopenInMigrateEVM(t *testing.T, dir string, batch int) *CompositeCommitStore {
	t.Helper()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(batch))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	return cs
}

func TestComposite_MigrateEVM_SecondNonEmptyFlushDoesNotAdvanceMigration(t *testing.T) {
	dir := t.TempDir()
	key1 := evmStorageTestKey(0x01)
	key2 := evmStorageTestKey(0x02)

	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = types.MemiavlOnly
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: key1, Value: evmStorageTestValue(0x11)},
			{Key: key2, Value: evmStorageTestValue(0x22)},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	cs = reopenInMigrateEVM(t, dir, 1)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: evmStorageTestKey(0x03), Value: evmStorageTestValue(0x33)},
		}}},
	}))
	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: evmStorageTestKey(0x04), Value: evmStorageTestValue(0x44)},
		}}},
	}))

	boundaryBytes, ok := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey))
	require.True(t, ok)
	boundary, err := migration.DeserializeMigrationBoundary(boundaryBytes)
	require.NoError(t, err)
	require.True(t, boundary.Equals(migration.NewMigrationBoundary(keys.EVMStoreKey, key1)),
		"second non-empty ApplyChangeSets in the same block must not migrate key2")

	_, ok = cs.flatKV.Get(keys.EVMStoreKey, key2)
	require.False(t, ok, "key2 should remain unmigrated until the next block")
}

func evmStorageTestKey(seed byte) []byte {
	addr := make([]byte, keys.AddressLen)
	slot := make([]byte, 32)
	for i := range addr {
		addr[i] = seed
	}
	for i := range slot {
		slot[i] = seed
	}
	return keys.BuildEVMKey(keys.EVMKeyStorage, append(addr, slot...))
}

func evmStorageTestValue(seed byte) []byte {
	value := make([]byte, 32)
	for i := range value {
		value[i] = seed
	}
	return value
}

func zeroStorageTestValue() []byte {
	return make([]byte, 32)
}

func isZeroTestValue(v []byte) bool {
	if len(v) == 0 {
		return true
	}
	for _, b := range v {
		if b != 0 {
			return false
		}
	}
	return true
}

func pruneZeroStorageViaRoutedGet(t *testing.T, cs *CompositeCommitStore, limit int) (int, int) {
	t.Helper()
	iter, err := cs.Iterator(keys.EVMStoreKey, keys.StateKeyPrefix(), []byte{0x04}, true)
	require.NoError(t, err)
	defer func() { require.NoError(t, iter.Close()) }()

	var deletes []*proto.KVPair
	processed := 0
	for ; iter.Valid() && processed < limit; iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		processed++

		// Match x/evm's migration-safe prune contract: iterator output is only a
		// candidate source; deletion is decided from the routed logical read path.
		value, found, err := cs.Get(keys.EVMStoreKey, key)
		require.NoError(t, err)
		if found && isZeroTestValue(value) {
			deletes = append(deletes, &proto.KVPair{Key: key, Delete: true})
		}
	}
	require.NoError(t, iter.Error())

	if len(deletes) > 0 {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name: keys.EVMStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: deletes,
			},
		}}))
	} else {
		require.NoError(t, cs.ApplyChangeSets(nil))
	}
	return processed, len(deletes)
}

func requireEVMStorageKeysAbsentFromIterator(t *testing.T, cs *CompositeCommitStore, absentKeys ...[]byte) {
	t.Helper()
	absent := make(map[string]struct{}, len(absentKeys))
	for _, key := range absentKeys {
		absent[string(key)] = struct{}{}
	}

	iter, err := cs.Iterator(keys.EVMStoreKey, keys.StateKeyPrefix(), []byte{0x04}, true)
	require.NoError(t, err)
	defer func() { require.NoError(t, iter.Close()) }()
	for ; iter.Valid(); iter.Next() {
		_, forbidden := absent[string(iter.Key())]
		require.Falsef(t, forbidden, "pruned zero-storage key %x must not appear in composite iterator", iter.Key())
	}
	require.NoError(t, iter.Error())
}

func TestComposite_MigrateEVM_PruneZeroStorageSlotsDuringMigration(t *testing.T) {
	dir := t.TempDir()
	zeroKeyBeforeBoundary := evmStorageTestKey(0x01)
	nonZeroKey := evmStorageTestKey(0x02)
	zeroKeyAfterBoundary := evmStorageTestKey(0x03)

	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = types.MemiavlOnly
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: zeroKeyBeforeBoundary, Value: zeroStorageTestValue()},
			{Key: nonZeroKey, Value: evmStorageTestValue(0x22)},
			{Key: zeroKeyAfterBoundary, Value: zeroStorageTestValue()},
		}},
	}}))
	_, err = cs.Commit()
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	cs = reopenInMigrateEVM(t, dir, 1)

	processed, deleted := pruneZeroStorageViaRoutedGet(t, cs, 10)
	require.Equal(t, 3, processed)
	require.Equal(t, 2, deleted)
	_, err = cs.Commit()
	require.NoError(t, err)

	for _, key := range [][]byte{zeroKeyBeforeBoundary, zeroKeyAfterBoundary} {
		value, found, err := cs.Get(keys.EVMStoreKey, key)
		require.NoError(t, err)
		require.Falsef(t, found, "pruned zero-storage key %x must be logically absent", key)
		require.Nil(t, value)
	}
	value, found, err := cs.Get(keys.EVMStoreKey, nonZeroKey)
	require.NoError(t, err)
	require.True(t, found, "non-zero storage key must survive prune")
	require.Equal(t, evmStorageTestValue(0x22), value)
	requireEVMStorageKeysAbsentFromIterator(t, cs, zeroKeyBeforeBoundary, zeroKeyAfterBoundary)

	workload := newMigrationWorkload(0x5150)
	runUntilMigrationComplete(t, cs, workload, 20)
	for _, key := range [][]byte{zeroKeyBeforeBoundary, zeroKeyAfterBoundary} {
		value, found, err := cs.Get(keys.EVMStoreKey, key)
		require.NoError(t, err)
		require.Falsef(t, found, "pruned zero-storage key %x must remain absent after migration completes", key)
		require.Nil(t, value)
	}
	value, found, err = cs.Get(keys.EVMStoreKey, nonZeroKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, evmStorageTestValue(0x22), value)
	requireEVMStorageKeysAbsentFromIterator(t, cs, zeroKeyBeforeBoundary, zeroKeyAfterBoundary)
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))

	preFlipVersion := cs.Version()
	preFlipHash := append([]byte(nil), cs.flatKV.CommittedRootHash()...)
	require.NoError(t, cs.Close())

	finalCfg := evmMigratedConfig()
	finalCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err = NewCompositeCommitStore(t.Context(), dir, finalCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.Equal(t, preFlipVersion, cs.Version())
	require.Equal(t, preFlipHash, cs.flatKV.CommittedRootHash())
	for _, key := range [][]byte{zeroKeyBeforeBoundary, zeroKeyAfterBoundary} {
		value, found, err := cs.Get(keys.EVMStoreKey, key)
		require.NoError(t, err)
		require.Falsef(t, found, "pruned zero-storage key %x must remain absent after EVMMigrated reopen", key)
		require.Nil(t, value)
	}
	value, found, err = cs.Get(keys.EVMStoreKey, nonZeroKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, evmStorageTestValue(0x22), value)
	requireEVMStorageKeysAbsentFromIterator(t, cs, zeroKeyBeforeBoundary, zeroKeyAfterBoundary)
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))
}

// runUntilMigrationComplete drives the workload through commits until
// the flatkv migration-version key reaches Version1_MigrateEVM. Fails
// the test if completion takes more than maxBlocks (guards against a
// silently mistuned batch size that would otherwise hang).
func runUntilMigrationComplete(
	t *testing.T,
	cs *CompositeCommitStore,
	workload *migrationWorkload,
	maxBlocks int,
) {
	t.Helper()
	for i := 0; i < maxBlocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(0, 2, 1, 1, 1)))
		_, err := cs.Commit()
		require.NoError(t, err)
		done, err := migration.IsAtVersion(flatKVReaderFor(cs), uint64(migration.Version1_MigrateEVM))
		require.NoError(t, err)
		if done {
			return
		}
	}
	t.Fatalf("migration did not complete within %d blocks", maxBlocks)
}

// requireOracleMatches asserts every (store, key) in oracle reads back
// via composite.Get with the expected value. Use this to validate the
// read-transparency invariant (I3) at any point in the lifecycle.
func requireOracleMatches(t *testing.T, cs *CompositeCommitStore, oracle map[migKeyPair][]byte) {
	t.Helper()
	for kp, want := range oracle {
		got, ok, err := cs.Get(kp.store, []byte(kp.key))
		require.NoError(t, err, "Get store=%q key=%x", kp.store, kp.key)
		require.True(t, ok, "expected present: store=%q key=%x", kp.store, kp.key)
		require.Equal(t, want, got, "value mismatch: store=%q key=%x", kp.store, kp.key)
	}
}

// requireEVMIteratorMatchesOracle iterates the composite's EVM store over
// the full logical key range and asserts the merged stream equals exactly
// the EVM entries in oracle, in ascending key order. During an in-flight
// migration the EVM keyspace is split across both backends (un-migrated
// keys in memiavl, migrated keys in flatkv), so this directly exercises
// composite.iterate's cross-backend stitching: the merged result must be a
// single, correctly-ordered, duplicate-free view of the union.
//
// The bounds [0x00, 0xff) cover every key the workload emits (storage-kind
// EVM keys all share the 0x03 prefix), and start/end are non-nil as
// composite.iterate requires.
func requireEVMIteratorMatchesOracle(t *testing.T, cs *CompositeCommitStore, oracle map[migKeyPair][]byte) {
	t.Helper()

	type kvPair struct{ k, v string }
	var want []kvPair
	for kp, v := range oracle {
		if kp.store == keys.EVMStoreKey {
			want = append(want, kvPair{kp.key, string(v)})
		}
	}
	sort.Slice(want, func(i, j int) bool { return want[i].k < want[j].k })

	iter, err := cs.Iterator(keys.EVMStoreKey, []byte{0x00}, []byte{0xff}, true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer func() { _ = iter.Close() }()

	var got []kvPair
	for ; iter.Valid(); iter.Next() {
		got = append(got, kvPair{string(iter.Key()), string(iter.Value())})
	}
	require.NoError(t, iter.Error())
	require.Equal(t, want, got,
		"composite EVM iteration must equal the oracle (merged memiavl+flatkv stream)")
}

// TestComposite_MigrateEVM_HappyPath drives the full MemiavlOnly ->
// MigrateEVM lifecycle through the production CompositeCommitStore
// entry point. The migration-package TestMigrateEVM covers the
// migration manager in isolation; this test pins the same invariants
// when traffic flows through the composite's Initialize / LoadVersion /
// ApplyChangeSets / Commit path, which additionally exercises
// migration-tree mounting on memiavl and the SetInitialVersion seeding
// that brings flatkv into lockstep on the mode flip.
func TestComposite_MigrateEVM_HappyPath(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xC0FFEE)

	const phase1Blocks = 20 // ~400 EVM keys (20 * 20)
	const phase2Blocks = 10 // stays in flight at batch=5
	const batch = 5

	driveMigrationWorkload(t, dir, workload, phase1Blocks, phase2Blocks, batch)

	cs := reopenInMigrateEVM(t, dir, batch)
	defer func() { _ = cs.Close() }()

	// Mid-flight sanity: phase 2 was sized to keep the boundary open.
	// If this fails, the test no longer exercises the partial-migration
	// hybrid read path, so tighten the batch or shorten phase 2.
	done, err := migration.IsAtVersion(flatKVReaderFor(cs), uint64(migration.Version1_MigrateEVM))
	require.NoError(t, err)
	require.False(t, done, "phase 2 should leave the migration in flight")

	// Current workload state (post phase-2 mutations) must still
	// resolve through the migration router after the close-and-reopen
	// cycle. This is the read-transparency invariant (I3): the boundary
	// between memiavl-resident and flatkv-resident EVM keys must not
	// be observable through the composite Get path.
	midFlightOracle := workload.snapshotOracle()
	requireOracleMatches(t, cs, midFlightOracle)

	// Same invariant for iteration: with the migration in flight the EVM
	// keyspace is split across memiavl (un-migrated) and flatkv
	// (migrated), so composite.Iterator must stitch both backends into a
	// single ordered stream that exactly equals the oracle.
	requireEVMIteratorMatchesOracle(t, cs, midFlightOracle)

	// Drive blocks until the boundary closes. 200 is generous; the
	// expected count is < 100 even with full churn.
	runUntilMigrationComplete(t, cs, workload, 200)

	finalOracle := workload.snapshotOracle()
	requireOracleMatches(t, cs, finalOracle)

	// Post-migration all EVM data lives in flatkv; iteration must still
	// return the full, correctly-ordered set from the (now sole) backend
	// holding the data.
	requireEVMIteratorMatchesOracle(t, cs, finalOracle)

	// I2: memiavl's evm tree must be empty post-migration. All evm
	// data lives in flatkv at this point; if memiavl still has any
	// keys here either the source deletes didn't fire or the migrator
	// gave up early.
	evmTree := cs.memIAVL.GetChildStoreByName(keys.EVMStoreKey)
	require.NotNil(t, evmTree)
	iter := evmTree.Iterator(nil, nil, true)
	t.Cleanup(func() { _ = iter.Close() })
	require.False(t, iter.Valid(),
		"post-migration memiavl evm tree must be empty (all data moved to flatkv)")

	// I4: full-scan lattice hash must agree with the stored committed
	// hash; this is the offline equivalent of the cross-validator
	// digest check the Docker tests run.
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV),
		"post-migration flatkv must pass full-scan LtHash verification")
}

// TestComposite_MigrateEVM_CrashAndResume models the most common
// in-flight restart scenario: an operator stops the node mid-migration
// (planned restart, node OOMs, deploy rollover) and brings it back up.
// The resume must be lossless: same final composite version, same
// flatkv committed root hash, same oracle as a no-restart control run.
//
// "Crash" here is a clean composite.Close mid-migration. That's the
// strongest scenario this layer can simulate without dropping
// commit-time disk writes, which would require reaching past the
// public composite API. The migration manager's mid-commit ordering
// is exercised elsewhere; this test focuses on the
// LoadVersion-after-restart resume path through the composite.
func TestComposite_MigrateEVM_CrashAndResume(t *testing.T) {
	const seed = int64(0xBADBEEF)
	const phase1Blocks = 15
	const phase2Blocks = 20
	const batch = 8

	runOnce := func(crashAfter int) (finalVersion int64, flatkvHash []byte, oracle map[migKeyPair][]byte) {
		dir := t.TempDir()
		workload := newMigrationWorkload(seed)

		memCfg := config.DefaultStateCommitConfig()
		memCfg.WriteMode = types.MemiavlOnly
		memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
		cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
		require.NoError(t, err)
		require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
		_, err = cs.LoadVersion(0, false)
		require.NoError(t, err)
		for i := 0; i < phase1Blocks; i++ {
			require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
			_, err := cs.Commit()
			require.NoError(t, err)
		}
		require.NoError(t, cs.Close())

		cs = reopenInMigrateEVM(t, dir, batch)

		runBlock := func() {
			require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(5, 5, 1, 2, 2)))
			_, err := cs.Commit()
			require.NoError(t, err)
		}

		// crashAfter <= 0 means the control run: drive all phase-2
		// blocks in one open-close cycle. Otherwise close after
		// crashAfter blocks and reopen to drive the rest, which is
		// what the resume path needs to be byte-equivalent to.
		if crashAfter > 0 {
			for i := 0; i < crashAfter; i++ {
				runBlock()
			}
			done, err := migration.IsAtVersion(flatKVReaderFor(cs), uint64(migration.Version1_MigrateEVM))
			require.NoError(t, err)
			require.False(t, done, "test must crash before migration completes; tighten crashAfter or batch")
			require.NoError(t, cs.Close())

			cs = reopenInMigrateEVM(t, dir, batch)
			for i := crashAfter; i < phase2Blocks; i++ {
				runBlock()
			}
		} else {
			for i := 0; i < phase2Blocks; i++ {
				runBlock()
			}
		}

		finalVersion = cs.Version()
		flatkvHash = append([]byte(nil), cs.flatKV.CommittedRootHash()...)
		oracle = workload.snapshotOracle()
		require.NoError(t, cs.Close())
		return
	}

	controlVer, controlHash, controlOracle := runOnce(0)
	resumeVer, resumeHash, resumeOracle := runOnce(phase2Blocks / 3)

	// Strongest correctness signal: the post-resume lattice state is
	// fully determined by the applied changeset sequence, so identical
	// input -> identical hash regardless of when the close-reopen
	// happened.
	require.Equal(t, controlVer, resumeVer,
		"resume must reach the same final version as the no-crash control")
	require.Equal(t, controlHash, resumeHash,
		"resume must produce the same flatkv committed root hash as the control")
	require.Equal(t, controlOracle, resumeOracle,
		"resume oracle must be byte-equivalent to control oracle (same seed)")
}

// TestComposite_MigrateEVM_DeterministicAcrossTwoStores asserts that
// two independent CompositeCommitStore instances driven by the same
// workload reach byte-identical flatkv committed root hashes at every
// block of the migration. This is the property a multi-validator chain
// depends on: if it ever fails here, validators will fork mid-migration.
//
// Two stores in two tempdirs, same workload seed, per-block hash
// comparison. The migration package's TestMigrateEVM verifies
// determinism only at the end of phase 3; lifting the check to every
// commit catches any non-determinism introduced after the first
// migration block (e.g. iteration-order drift in the batch copier).
func TestComposite_MigrateEVM_DeterministicAcrossTwoStores(t *testing.T) {
	const seed = int64(0xD37E12)
	const phase1Blocks = 15
	const phase2Blocks = 60 // enough at batch=5 to span the full migration
	const batch = 5

	run := func() (finalVersion int64, perBlockHashes [][]byte) {
		dir := t.TempDir()
		workload := newMigrationWorkload(seed)

		memCfg := config.DefaultStateCommitConfig()
		memCfg.WriteMode = types.MemiavlOnly
		memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
		cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
		require.NoError(t, err)
		require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
		_, err = cs.LoadVersion(0, false)
		require.NoError(t, err)
		for i := 0; i < phase1Blocks; i++ {
			require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(20, 0, 0, 5, 0)))
			_, err := cs.Commit()
			require.NoError(t, err)
		}
		require.NoError(t, cs.Close())

		cs = reopenInMigrateEVM(t, dir, batch)
		defer func() { _ = cs.Close() }()

		perBlockHashes = make([][]byte, 0, phase2Blocks)
		for i := 0; i < phase2Blocks; i++ {
			require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(5, 5, 1, 2, 2)))
			_, err := cs.Commit()
			require.NoError(t, err)
			perBlockHashes = append(perBlockHashes, append([]byte(nil), cs.flatKV.CommittedRootHash()...))
		}
		finalVersion = cs.Version()
		return
	}

	verA, hashesA := run()
	verB, hashesB := run()

	require.Equal(t, verA, verB,
		"two independent runs of the same workload must reach the same final version")
	require.Equal(t, len(hashesA), len(hashesB))
	for i := range hashesA {
		require.Equalf(t, hashesA[i], hashesB[i],
			"phase-2 block %d (composite version %d): flatkv committed root hash differs between runs",
			i, int64(phase1Blocks)+int64(i)+1)
	}
}

// TestComposite_MigrateEVM_PostCompletionFlipToEVMMigrated exercises
// the production mode flip sequence: once the migration boundary closes
// the operator flips sc-write-mode from migrate_evm to evm_migrated to
// stop spinning up a MigrationManager on every restart. The flip must
// be lossless on disk (same version, same flatkv hash, same oracle)
// and new EVM writes must continue to land directly in flatkv.
func TestComposite_MigrateEVM_PostCompletionFlipToEVMMigrated(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xDA7A)

	const phase1Blocks = 10
	const phase2Blocks = 5
	const batch = 6

	driveMigrationWorkload(t, dir, workload, phase1Blocks, phase2Blocks, batch)

	// Reopen in MigrateEVM and run to completion, capturing the
	// pre-migration state for the lossless-flip assertions below.
	cs := reopenInMigrateEVM(t, dir, batch)
	runUntilMigrationComplete(t, cs, workload, 200)

	preFlipVersion := cs.Version()
	preFlipOracle := workload.snapshotOracle()
	preFlipFlatkvHash := append([]byte(nil), cs.flatKV.CommittedRootHash()...)
	require.NoError(t, cs.Close())

	// --- Mode flip: reopen as EVMMigrated. ---
	finalCfg := evmMigratedConfig()
	finalCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err := NewCompositeCommitStore(t.Context(), dir, finalCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.Equal(t, preFlipVersion, cs.Version(),
		"EVMMigrated reopen must report the same version as the completed MigrateEVM run")
	require.Equal(t, preFlipFlatkvHash, cs.flatKV.CommittedRootHash(),
		"flatkv committed root hash must be invariant across the MigrateEVM -> EVMMigrated mode flip")
	requireOracleMatches(t, cs, preFlipOracle)

	// Post-migration writes must continue to land in flatkv and remain
	// readable. This catches the regression where a post-flip mode
	// accidentally routes EVM writes to memiavl, which would leave a
	// silent split between authoritative state (flatkv) and new state
	// (memiavl) that no read path can heal.
	postFlipBlock := workload.generateBlock(5, 3, 1, 2, 1)
	require.NoError(t, cs.ApplyChangeSets(postFlipBlock))
	_, err = cs.Commit()
	require.NoError(t, err)
	requireOracleMatches(t, cs, workload.snapshotOracle())
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))

	// EVMMigrated has no migration manager, so memiavl's evm tree must
	// still be empty after a post-flip block (writes went to flatkv).
	evmTree := cs.memIAVL.GetChildStoreByName(keys.EVMStoreKey)
	require.NotNil(t, evmTree)
	iter := evmTree.Iterator(nil, nil, true)
	t.Cleanup(func() { _ = iter.Close() })
	require.False(t, iter.Valid(),
		"post-flip memiavl evm tree must remain empty (EVM writes route to flatkv)")
}

// cloneCommitInfo deep-copies a *proto.CommitInfo so a captured snapshot
// survives later commits / reopens that mutate the live store.
func cloneCommitInfo(ci *proto.CommitInfo) *proto.CommitInfo {
	if ci == nil {
		return nil
	}
	out := &proto.CommitInfo{
		Version:    ci.Version,
		StoreInfos: make([]proto.StoreInfo, len(ci.StoreInfos)),
	}
	for i, si := range ci.StoreInfos {
		out.StoreInfos[i] = proto.StoreInfo{
			Name: si.Name,
			CommitId: proto.CommitID{
				Version: si.CommitId.Version,
				Hash:    append([]byte(nil), si.CommitId.Hash...),
			},
		}
	}
	return out
}

// requireCommitInfoEqual asserts two commit infos carry the same version
// and the same per-store hashes. It compares name->hex maps so a failure
// names exactly which store (e.g. evm_lattice vs a memiavl module)
// diverged and by how much, rather than dumping opaque byte slices.
func requireCommitInfoEqual(t *testing.T, want, got *proto.CommitInfo, msg string) {
	t.Helper()
	require.NotNilf(t, want, "%s: no canonical commit info captured", msg)
	require.NotNilf(t, got, "%s: got nil commit info", msg)
	require.Equalf(t, want.Version, got.Version, "%s: version", msg)

	hashByName := func(ci *proto.CommitInfo) map[string]string {
		m := make(map[string]string, len(ci.StoreInfos))
		for _, si := range ci.StoreInfos {
			m[si.Name] = hex.EncodeToString(si.CommitId.Hash)
		}
		return m
	}
	require.Equalf(t, hashByName(want), hashByName(got), "%s: per-store hashes", msg)
}

// rollbackSnapSettings selects the snapshot cadence applied to both
// backends in the rollback reproduction test. Zero disables periodic
// snapshots (everything replays from the genesis snapshot); a small
// positive interval forces snapshots mid-run so rollback must seek a
// snapshot below the target and discard snapshots above it.
type rollbackSnapSettings struct {
	memiavlInterval uint32
	flatkvInterval  uint32
}

// openCompositeForRollback opens a composite store with deterministic,
// synchronous commit settings and the requested snapshot cadence. Used
// by the rollback reproduction test so the same forward/rollback driver
// runs with and without snapshot boundaries straddling the target.
func openCompositeForRollback(
	t *testing.T, dir string, mode types.WriteMode, batch int, snap rollbackSnapSettings,
) *CompositeCommitStore {
	t.Helper()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = mode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cfg.MemIAVLConfig.SnapshotInterval = snap.memiavlInterval
	// Disable the time throttle so snapshots fire on the block interval
	// alone, and keep several so the rollback target sits below the
	// newest snapshot but above an older retained one.
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.SnapshotKeepRecent = 5
	cfg.FlatKVConfig.SnapshotInterval = snap.flatkvInterval
	cfg.FlatKVConfig.SnapshotKeepRecent = 5

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(batch))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	return cs
}

// TestComposite_MigrateEVM_RollbackAcrossActivationBoundary reproduces
// the rollback failure at the memiavl-only -> migrate_evm activation
// boundary. Blocks before the migration boundary commits are memiavl-only
// AppHash blocks (no evm_lattice); once the boundary opens, later AppHashes
// include the lattice. CompositeCommitStore.latticeAppendLatched latches to
// true the first time it observes the open boundary. A node opened at a
// post-activation height (latch=true) that is rolled back before activation
// must invalidate that latch, or LastCommitInfo wrongly appends the stale
// lattice and produces a non-canonical AppHash.
func TestComposite_MigrateEVM_RollbackAcrossActivationBoundary(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0x5701)

	const phase1Blocks = 8
	const batch = 4

	// Phase 1: MemiavlOnly bootstrap.
	cs := openCompositeForRollback(t, dir, types.MemiavlOnly, batch, rollbackSnapSettings{})
	for i := 0; i < phase1Blocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(30, 0, 0, 5, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	// Capture the canonical pre-activation commit info. It must not carry
	// the lattice because all consensus state still lives in memiavl.
	target := int64(phase1Blocks)
	canonicalTarget := cloneCommitInfo(cs.LastCommitInfo())
	require.False(t, hasLattice(canonicalTarget),
		"sanity: canonical pre-activation commit info must not contain evm_lattice")
	require.NoError(t, cs.Close())

	// Phase 2: MigrateEVM. Capture the canonical commit info at each
	// version. Versions after activation must include the lattice.
	cs = openCompositeForRollback(t, dir, types.MigrateEVM, batch, rollbackSnapSettings{})
	canonical := map[int64]*proto.CommitInfo{}
	const phase2Blocks = 10
	for i := 0; i < phase2Blocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(8, 4, 1, 2, 1)))
		v, err := cs.Commit()
		require.NoError(t, err)
		canonical[v] = cloneCommitInfo(cs.LastCommitInfo())
	}
	finalVer := cs.Version()
	require.NoError(t, cs.Close())

	require.Contains(t, canonical, finalVer)
	require.True(t, hasLattice(canonical[finalVer]),
		"sanity: canonical post-activation commit info must contain evm_lattice")

	// Reopen fresh at the latest (post-H_start) height, then roll back
	// across the boundary exactly as `seid rollback` would.
	//
	// The CLI reads the latest commit info before rolling back (it
	// prints "Initial App state height=.. hash=.."), and
	// rootmulti.loadVersion itself calls scStore.LastCommitInfo()
	// during load. Either path observes the open migration boundary at
	// the post-activation height and latches latticeAppendLatched=true.
	// We reproduce that read here so the rollback runs with the flag
	// already latched, as it is in production.
	cs = openCompositeForRollback(t, dir, types.MigrateEVM, batch, rollbackSnapSettings{})
	preRollback := cs.LastCommitInfo()
	require.True(t, hasLattice(preRollback),
		"sanity: latest commit info is post-activation and must carry the lattice (this latches the flag)")

	require.NoError(t, cs.Rollback(target))
	require.Equal(t, target, cs.Version())

	// In-process post-rollback view: the AppHash the `seid rollback` CLI
	// prints and the value rootmulti caches in rs.lastCommitInfo. Before
	// the latch reset in Rollback this wrongly included the evm_lattice
	// for a pre-activation height.
	require.False(t, hasLattice(cs.LastCommitInfo()),
		"post-rollback commit info for a pre-activation height must not carry evm_lattice")
	requireCommitInfoEqual(t, canonicalTarget, cs.LastCommitInfo(),
		"post-rollback LastCommitInfo across the activation boundary (same process)")
	require.NoError(t, cs.Close())

	// Post-restart view: the AppHash `seid start` hands to Tendermint.
	cs = openCompositeForRollback(t, dir, types.MigrateEVM, batch, rollbackSnapSettings{})
	defer func() { _ = cs.Close() }()
	require.Equal(t, target, cs.Version())
	requireCommitInfoEqual(t, canonicalTarget, cs.LastCommitInfo(),
		"post-restart LastCommitInfo across the activation boundary (fresh process)")
}

// hasLattice reports whether a commit info carries the synthetic
// evm_lattice store info contributed by the flatkv backend.
func hasLattice(ci *proto.CommitInfo) bool {
	if ci == nil {
		return false
	}
	for _, si := range ci.StoreInfos {
		if si.Name == "evm_lattice" {
			return true
		}
	}
	return false
}

// TestComposite_MigrateEVM_RollbackRestoresCanonicalCommitInfo pins the
// invariant that `seid rollback` violated on the mid-migration hstart-2
// node: rolling a migrate_evm composite store back to version T must
// reproduce the exact commit info (and therefore AppHash) the chain
// committed at T during forward progress. If the post-rollback
// LastCommitInfo diverges, Tendermint's handshake at restart fails with
// "Did you reset Tendermint without resetting your application's data?"
// because the app's AppHash no longer matches the value Tendermint
// recorded for that height.
//
// The test captures the canonical commit info at every committed version
// during forward migration, then reopens fresh (mirroring the CLI
// constructing the app and loading latest), rolls back into the middle
// of the run, and asserts the commit info matches — both immediately
// after Rollback (the hash the CLI prints) and after a subsequent reopen
// (the hash `seid start` hands to Tendermint).
//
// The snapshots_across_boundary subtest reproduces the production
// conditions most likely to break rollback: both backends took periodic
// snapshots, so the rollback target lands between an older retained
// snapshot and newer snapshots that must be discarded and replayed past.
func TestComposite_MigrateEVM_RollbackRestoresCanonicalCommitInfo(t *testing.T) {
	cases := []struct {
		name string
		snap rollbackSnapSettings
	}{
		{name: "no_periodic_snapshots", snap: rollbackSnapSettings{}},
		{name: "snapshots_across_boundary", snap: rollbackSnapSettings{memiavlInterval: 2, flatkvInterval: 2}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			workload := newMigrationWorkload(0x5701)

			const phase1Blocks = 8
			const phase2Blocks = 16
			const batch = 4

			// Phase 1: MemiavlOnly bootstrap so there is pre-migration
			// state living on memiavl when the boundary opens.
			cs := openCompositeForRollback(t, dir, types.MemiavlOnly, batch, tc.snap)
			for i := 0; i < phase1Blocks; i++ {
				require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(30, 0, 0, 5, 0)))
				_, err := cs.Commit()
				require.NoError(t, err)
			}
			require.NoError(t, cs.Close())

			// Phase 2: MigrateEVM. Record the canonical commit info at
			// each committed version. A small batch relative to the live
			// EVM key count keeps the migration in progress across the
			// whole run, so every captured version carries the
			// evm_lattice store info.
			cs = openCompositeForRollback(t, dir, types.MigrateEVM, batch, tc.snap)
			canonical := map[int64]*proto.CommitInfo{}
			for i := 0; i < phase2Blocks; i++ {
				require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(8, 4, 1, 2, 1)))
				v, err := cs.Commit()
				require.NoError(t, err)
				canonical[v] = cloneCommitInfo(cs.LastCommitInfo())
			}

			done, err := migration.IsAtVersion(flatKVReaderFor(cs), uint64(migration.Version1_MigrateEVM))
			require.NoError(t, err)
			require.False(t, done,
				"migration must still be in progress for this test to exercise the migrate_evm rollback path")

			finalVer := cs.Version()
			require.NoError(t, cs.Close())

			target := finalVer - 5
			require.Contains(t, canonical, target, "target must be a captured version")

			// Reopen fresh, mirroring `seid rollback` building the app
			// via appCreator and LoadVersion(0), then perform the
			// rollback exactly as rootmulti.RollbackToVersion does.
			cs = openCompositeForRollback(t, dir, types.MigrateEVM, batch, tc.snap)
			require.NoError(t, cs.Rollback(target))
			require.Equal(t, target, cs.Version())

			requireCommitInfoEqual(t, canonical[target], cs.LastCommitInfo(),
				"post-rollback LastCommitInfo (the AppHash the CLI prints)")
			require.NoError(t, cs.Close())

			// Reopen once more, mirroring `seid start` after the
			// rollback. The AppHash Tendermint validates during the
			// handshake is derived from this freshly-loaded
			// LastCommitInfo.
			cs = openCompositeForRollback(t, dir, types.MigrateEVM, batch, tc.snap)
			defer func() { _ = cs.Close() }()
			require.Equal(t, target, cs.Version())
			requireCommitInfoEqual(t, canonical[target], cs.LastCommitInfo(),
				"post-restart LastCommitInfo (the AppHash `seid start` hands to Tendermint)")
		})
	}
}

// TestComposite_MigrateBank_RollbackAcrossCompletionBoundary is the
// memiavl-side mirror of TestComposite_MigrateEVM_RollbackAcrossActivationBoundary.
// Once the bank migration completes (version 3) a store with memiavl still
// open latches memiavlHashExcluded=true and drops memiavl's per-store infos
// from the commit info (the flatkv_only shape). Rolling back below the
// completion boundary must re-include memiavl, or the in-process
// post-rollback AppHash diverges from the canonical value the chain
// committed at that height (which still carries the memiavl stores).
func TestComposite_MigrateBank_RollbackAcrossCompletionBoundary(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0xB0A7)
	const batch = 8

	cs := openAutoStore(t, dir, batch)

	// Bootstrap bank+evm state under MemiavlOnly (bank-heavy so migrate_bank
	// later spans several blocks), then walk the ladder to migrate_bank.
	for i := 0; i < 12; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(6, 0, 0, 40, 0)))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 400)
	require.NoError(t, cs.SetWriteMode(types.EVMMigrated))
	runBlocks(t, cs, workload, 2)
	require.NoError(t, cs.SetWriteMode(types.MigrateAllButBank))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version2_MigrateAllButBank, 400)
	require.NoError(t, cs.SetWriteMode(types.AllMigratedButBank))
	runBlocks(t, cs, workload, 2)

	require.NoError(t, cs.SetWriteMode(types.MigrateBank))
	require.Equal(t, types.MigrateBank, cs.currentWriteMode)

	// One migrate_bank block that does not complete the migration: memiavl
	// is still part of the AppHash here. Capture it as the rollback target.
	require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(0, 2, 1, 0, 2)))
	_, err := cs.Commit()
	require.NoError(t, err)
	target := cs.Version()
	canonicalTarget := cloneCommitInfo(cs.LastCommitInfo())
	require.Greater(t, len(canonicalTarget.StoreInfos), 1,
		"sanity: migrate_bank in-progress commit info must still include memiavl stores")

	// Drive to bank completion while staying in currentWriteMode
	// MigrateBank (do NOT flip to FlatKVOnly), so the rollback exercises the
	// latch reset rather than a mode change. Reaching version 3 latches
	// memiavlHashExcluded=true and drops memiavl from the commit info.
	runUntilAtMigrationVersion(t, cs, workload, migration.Version3_FlatKVOnly, 800)
	require.Equal(t, []string{"evm_lattice"}, storeInfoNames(cs),
		"once bank migration completes, memiavl infos are excluded (latches memiavlHashExcluded)")

	// Roll back across the completion boundary. Without resetting
	// memiavlHashExcluded the post-rollback commit info would keep the
	// flatkv_only shape and drop memiavl.
	require.NoError(t, cs.Rollback(target))
	require.Equal(t, target, cs.Version())
	requireCommitInfoEqual(t, canonicalTarget, cs.LastCommitInfo(),
		"post-rollback commit info across the bank-completion boundary must re-include memiavl")
	require.NoError(t, cs.Close())

	// Fresh restart re-derives from the rolled-back metadata and must agree.
	cs = openAutoStore(t, dir, batch)
	defer func() { _ = cs.Close() }()
	require.Equal(t, target, cs.Version())
	requireCommitInfoEqual(t, canonicalTarget, cs.LastCommitInfo(),
		"post-restart commit info across the bank-completion boundary must re-include memiavl")
}
