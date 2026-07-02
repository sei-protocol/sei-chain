package migration

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

var _ Router = (*TestMultiDB)(nil)

// This database is for testing migration. It allows for us to apply operations to multiple "databases" in parallel.
// Afterwards, we can verify that each database contains the same data.
type TestMultiDB struct {
	// Nested DBs. Each operation is passed to each of the nested databases.
	nestedDBs []Router
}

// NewTestMultiDB creates a new TestMultiDB with the given nested databases.
func NewTestMultiDB(nestedDBs ...Router) *TestMultiDB {
	return &TestMultiDB{nestedDBs: nestedDBs}
}

// NewTestMultiRouter is an alias for [NewTestMultiDB] that accepts a *testing.T
// for symmetry with the other test-router constructors.
func NewTestMultiRouter(_ *testing.T, nestedDBs ...Router) *TestMultiDB {
	return NewTestMultiDB(nestedDBs...)
}

func (m *TestMultiDB) ApplyChangeSets(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error {
	for _, nestedDB := range m.nestedDBs {
		err := nestedDB.ApplyChangeSets(changesets, firstBatchInBlock)
		if err != nil {
			return fmt.Errorf("failed to apply changes to nested database %q: %w", nestedDB, err)
		}
	}
	return nil
}

func (m *TestMultiDB) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	// The multi-DB utility does not support testing of state proofs.
	panic("not implemented")
}

func (m *TestMultiDB) SetMigrationBatchSize(batchSize int) {
	for _, nestedDB := range m.nestedDBs {
		nestedDB.SetMigrationBatchSize(batchSize)
	}
}

func (m *TestMultiDB) Read(store string, key []byte) ([]byte, bool, error) {
	if len(m.nestedDBs) == 0 {
		return nil, false, fmt.Errorf("no nested databases configured")
	}
	values := make([][]byte, 0, len(m.nestedDBs))
	founds := make([]bool, 0, len(m.nestedDBs))
	for i, nestedDB := range m.nestedDBs {
		v, ok, err := nestedDB.Read(store, key)
		if err != nil {
			return nil, false, fmt.Errorf("failed to read from nested database %d: %w", i, err)
		}
		values = append(values, v)
		founds = append(founds, ok)
	}
	for i := 1; i < len(m.nestedDBs); i++ {
		if founds[i] != founds[0] || !bytes.Equal(values[i], values[0]) {
			return nil, false, fmt.Errorf(
				"nested database %d returned a different value for store %q key %x: got (%x, %t), expected (%x, %t)",
				i, store, key, values[i], founds[i], values[0], founds[0],
			)
		}
	}
	return values[0], founds[0], nil
}

// TestFlatKVRouter is a [Router] that sends all operations to a single underlying
// flatkv.CommitStore. It does not support iteration or proofs.
type TestFlatKVRouter struct {
	flatKV *flatkv.CommitStore
}

var _ Router = (*TestFlatKVRouter)(nil)

func NewTestFlatKVRouter(_ *testing.T, flatKV *flatkv.CommitStore) *TestFlatKVRouter {
	return &TestFlatKVRouter{flatKV: flatKV}
}

func (r *TestFlatKVRouter) Read(store string, key []byte) ([]byte, bool, error) {
	value, found := r.flatKV.Get(store, key)
	return value, found, nil
}

func (r *TestFlatKVRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, _ bool) error {
	return r.flatKV.ApplyChangeSets(changesets)
}

func (r *TestFlatKVRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestFlatKVRouter does not support proofs")
}

func (r *TestFlatKVRouter) SetMigrationBatchSize(int) {}

// TestMemIAVLRouter is a [Router] that sends all operations to a single underlying
// memiavl.CommitStore. It does not support iteration or proofs.
type TestMemIAVLRouter struct {
	memIAVL *memiavl.CommitStore
}

var _ Router = (*TestMemIAVLRouter)(nil)

func NewTestMemIAVLRouter(_ *testing.T, memIAVL *memiavl.CommitStore) *TestMemIAVLRouter {
	return &TestMemIAVLRouter{memIAVL: memIAVL}
}

func (r *TestMemIAVLRouter) Read(store string, key []byte) ([]byte, bool, error) {
	childStore := r.memIAVL.GetChildStoreByName(store)
	if childStore == nil {
		return nil, false, fmt.Errorf("store not found: %s", store)
	}
	value := childStore.Get(key)
	return value, value != nil, nil
}

func (r *TestMemIAVLRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, _ bool) error {
	return r.memIAVL.ApplyChangeSets(changesets)
}

func (r *TestMemIAVLRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestMemIAVLRouter does not support proofs")
}

func (r *TestMemIAVLRouter) SetMigrationBatchSize(int) {}

// TestInMemoryRouter is a [Router] backed by an in-memory map. The outer map keys
// are store (module) names and the inner map keys are store keys. It does not
// support iteration or proofs.
type TestInMemoryRouter struct {
	stores map[string]map[string][]byte
}

var _ Router = (*TestInMemoryRouter)(nil)

func NewTestInMemoryRouter() *TestInMemoryRouter {
	return &TestInMemoryRouter{stores: make(map[string]map[string][]byte)}
}

func (r *TestInMemoryRouter) Read(store string, key []byte) ([]byte, bool, error) {
	storeMap, ok := r.stores[store]
	if !ok {
		return nil, false, nil
	}
	value, ok := storeMap[string(key)]
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

func (r *TestInMemoryRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, _ bool) error {
	for _, ncs := range changesets {
		if ncs == nil {
			continue
		}
		storeMap, ok := r.stores[ncs.Name]
		if !ok {
			storeMap = make(map[string][]byte)
			r.stores[ncs.Name] = storeMap
		}
		for _, pair := range ncs.Changeset.Pairs {
			if pair == nil {
				continue
			}
			if pair.Delete {
				delete(storeMap, string(pair.Key))
				continue
			}
			storeMap[string(pair.Key)] = append([]byte(nil), pair.Value...)
		}
	}
	return nil
}

func (r *TestInMemoryRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestInMemoryRouter does not support proofs")
}

func (r *TestInMemoryRouter) SetMigrationBatchSize(int) {}

// VerifyKeyPlacement verifies that every key in the oracle is in the correct backend.
// Keys whose store name appears in flatKVStores must be readable from flatKVRouter and
// absent from memiavlRouter. All other keys must be in memiavlRouter and absent from
// flatKVRouter.
func (r *TestInMemoryRouter) VerifyKeyPlacement(
	t *testing.T,
	memIAVL *memiavl.CommitStore,
	flatKV *flatkv.CommitStore,
	// Map of store name to whether that value should be present in flatKV. If true for a store,
	// all keys in that store should be present in flatKV and absent from memIAVL.
	// If false or not present, all keys in that store should be present in memIAVL and absent from flatKV.
	flatKVStores map[string]bool,
) {
	t.Helper()

	memIAVLGet := func(store string, key []byte) ([]byte, bool) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, false
		}
		v := childStore.Get(key)
		return v, v != nil
	}

	for storeName, storeMap := range r.stores {
		for k, expected := range storeMap {
			key := []byte(k)
			if flatKVStores[storeName] {
				val, found := flatKV.Get(storeName, key)
				require.True(t, found, "store %q key %x should be in flatKV", storeName, key)
				require.Equal(t, expected, val, "store %q key %x value mismatch in flatKV", storeName, key)
				_, found = memIAVLGet(storeName, key)
				require.False(t, found, "store %q key %x should have been removed from memiavl", storeName, key)
			} else {
				val, found := memIAVLGet(storeName, key)
				require.True(t, found, "store %q key %x should be in memiavl", storeName, key)
				require.Equal(t, expected, val, "store %q key %x value mismatch in memiavl", storeName, key)
				_, found = flatKV.Get(storeName, key)
				require.False(t, found, "store %q key %x should not be in flatKV", storeName, key)
			}
		}
	}
}

// AssertMigrationInFlight verifies that, across the given migratingStores,
// at least one tracked key is still in memiavl (un-migrated) AND at least
// one tracked key is already in flatKV (migrated). Use this immediately
// before a mid-migration restart to confirm the test actually exercises
// the in-flight resume path; if either side is empty, the migration either
// completed or never started before the restart point and the test is not
// covering what it claims to cover.
//
// migratingStores is the set of source stores currently being migrated
// from memiavl to flatKV (e.g. {EVMStoreKey} during MigrateEVM).
func (r *TestInMemoryRouter) AssertMigrationInFlight(
	t *testing.T,
	memIAVL *memiavl.CommitStore,
	flatKV *flatkv.CommitStore,
	migratingStores ...string,
) {
	t.Helper()
	targets := make(map[string]bool, len(migratingStores))
	for _, s := range migratingStores {
		targets[s] = true
	}
	var foundInMemiavl, foundInFlatKV bool
outer:
	for storeName, storeMap := range r.stores {
		if !targets[storeName] {
			continue
		}
		childStore := memIAVL.GetChildStoreByName(storeName)
		for k := range storeMap {
			key := []byte(k)
			if !foundInMemiavl && childStore != nil && childStore.Get(key) != nil {
				foundInMemiavl = true
			}
			if !foundInFlatKV {
				if _, ok := flatKV.Get(storeName, key); ok {
					foundInFlatKV = true
				}
			}
			if foundInMemiavl && foundInFlatKV {
				break outer
			}
		}
	}
	require.True(t, foundInMemiavl,
		"expected at least one un-migrated key in memiavl across stores %v; "+
			"migration appears to have completed before the restart point - "+
			"reduce phase-2 length or increase phase-1 source-key volume",
		migratingStores)
	require.True(t, foundInFlatKV,
		"expected at least one migrated key in flatKV across stores %v; "+
			"migration appears not to have started before the restart point - "+
			"increase phase-2 length or reduce migration batch size",
		migratingStores)
}

// Tests to see if this in-memory database is equal to the data produced by the given router.
//
// For every (store, key) tracked by this router, the given router must return the same value with
// found=true. Keys held by the other router but unknown to this one cannot be detected, since
// [Router] does not expose iteration.
func (r *TestInMemoryRouter) VerifyContainsSameData(t *testing.T, that Router) {
	t.Helper()
	for storeName, storeMap := range r.stores {
		for k, expected := range storeMap {
			key := []byte(k)
			actual, found, err := that.Read(storeName, key)
			require.NoError(t, err, "reading store %q key %x", storeName, key)
			require.True(t, found, "store %q key %x: expected value %x, but other router reported not found",
				storeName, key, expected)
			require.Equal(t, expected, actual, "store %q key %x: value mismatch", storeName, key)
		}
	}
}

// GetFlatKVKeyCount returns the raw physical key count across all data DBs in
// flatKV. Pending (uncommitted) writes are not included.
//
// "Raw" means this counts physical DB rows, not logical application keys.
// flatKV merges nonce, code-hash, and (future) balance into a single physical
// account row, so the physical count will differ from the memiavl logical count
// for those key types. In other words, this count will match the logical key
// count only as long as there are no accidental collisions of keys with
// different types for the same account address (e.g. a nonce key and a
// codehash key that share the same randomly-generated address). Given that
// addresses are drawn from 20 random bytes (~1.2 × 10⁴⁸ possible values) the
// probability of such a collision in a test is negligible.
func GetFlatKVKeyCount(t *testing.T, flatKV *flatkv.CommitStore) int64 {
	t.Helper()
	iter, err := flatKV.RawGlobalIterator()
	require.NoError(t, err)
	defer func() { _ = iter.Close() }()
	var count int64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	require.NoError(t, iter.Error())
	return count
}

// GetMemIAVLKeyCount returns the total number of keys stored across every tree
// in the given memIAVL CommitStore.
func GetMemIAVLKeyCount(t *testing.T, memIAVL *memiavl.CommitStore) int64 {
	t.Helper()
	var total int64
	for _, namedTree := range memIAVL.GetDB().Trees() {
		iter := namedTree.Iterator(nil, nil, true)
		for ; iter.Valid(); iter.Next() {
			total++
		}
		require.NoError(t, iter.Error(), "iterator error on tree %q", namedTree.Name)
		_ = iter.Close()
	}
	return total
}

// ReadMigrationVersionFromFlatKV reads the stored migration version from flatKV's
// MigrationStore. Returns (version, true) if the key is present, (0, false) if absent.
func ReadMigrationVersionFromFlatKV(t *testing.T, flatKV *flatkv.CommitStore) (uint64, bool) {
	t.Helper()
	v, ok, err := readVersionFromDB(buildFlatKVReader(flatKV))
	require.NoError(t, err, "ReadMigrationVersionFromFlatKV")
	return v, ok
}

// ReadMigrationBoundaryFromFlatKV reads MigrationBoundaryKey from flatKV's
// MigrationStore. Returns (rawValue, true) if the key is present, (nil, false)
// if absent. The raw bytes are returned without decoding because callers
// generally only care whether the key exists post-migration.
func ReadMigrationBoundaryFromFlatKV(t *testing.T, flatKV *flatkv.CommitStore) ([]byte, bool) {
	t.Helper()
	v, ok, err := buildFlatKVReader(flatKV)(MigrationStore, []byte(MigrationBoundaryKey))
	require.NoError(t, err, "ReadMigrationBoundaryFromFlatKV")
	return v, ok
}

// encodeVersion serialises a migration version using the same big-endian
// uint64 layout the migration manager writes to MigrationVersionKey
// (see migration_manager.go's final-block change set construction).
func encodeVersion(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// SeedMigrationVersionInFlatKV writes MigrationVersionKey to flatKV's
// MigrationStore at the given version and commits. Use this to set up
// preconditions for migrations that start at a non-zero version
// (e.g. building a v1 -> v2 migration test on top of an EVMMigrated
// setup that has not itself written the version key).
func SeedMigrationVersionInFlatKV(t *testing.T, flatKV *flatkv.CommitStore, version uint64) {
	t.Helper()
	require.NoError(t, flatKV.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: MigrationStore,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte(MigrationVersionKey), Value: encodeVersion(version)},
		}},
	}}), "seed migration version")
	_, err := flatKV.Commit()
	require.NoError(t, err, "commit after seeding migration version")
}

type keyPair struct {
	store string
	key   string
}

// liveKeySet is a set of keyPair values that supports O(1) Add, O(1) Remove,
// and O(n) deterministic random sampling without relying on map-iteration
// order. The underlying slice is the source of truth for sampling; the map is
// a back-pointer used to make Remove O(1).
type liveKeySet struct {
	keys []keyPair
	idx  map[keyPair]int
}

func newLiveKeySet() *liveKeySet {
	return &liveKeySet{idx: make(map[keyPair]int)}
}

func (s *liveKeySet) Len() int { return len(s.keys) }

// Add inserts kp into the set. No-op if kp is already present.
func (s *liveKeySet) Add(kp keyPair) {
	if _, ok := s.idx[kp]; ok {
		return
	}
	s.idx[kp] = len(s.keys)
	s.keys = append(s.keys, kp)
}

// Remove deletes kp from the set in O(1) by swapping with the last element
// and popping. No-op if kp is not present.
func (s *liveKeySet) Remove(kp keyPair) {
	i, ok := s.idx[kp]
	if !ok {
		return
	}
	last := len(s.keys) - 1
	if i != last {
		s.keys[i] = s.keys[last]
		s.idx[s.keys[i]] = i
	}
	s.keys = s.keys[:last]
	delete(s.idx, kp)
}

// Sample returns up to n distinct keyPairs uniformly at random. If the set has
// fewer than n entries, all entries are returned. The result depends only on
// the contents of s.keys and the calls made to r — no map iteration is
// involved, so output is fully reproducible from r's seed.
//
// Implementation: Floyd's algorithm for selecting an n-subset, which runs in
// O(n) time and never mutates s.keys.
func (s *liveKeySet) Sample(r *testutil.TestRandom, n int) []keyPair {
	population := len(s.keys)
	if n > population {
		n = population
	}
	if n == 0 {
		return nil
	}
	chosen := make(map[int]struct{}, n)
	out := make([]keyPair, 0, n)
	for i := population - n; i < population; i++ {
		j := r.Intn(i + 1)
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

// randomTestBytes returns n random bytes suitable for use as a test key or value.
// Uses the supplied *testutil.TestRandom so output is deterministic given a seed.
func randomTestBytes(rng *testutil.TestRandom, n int) []byte {
	return rng.Bytes(n)
}

// randomEVMKVPair returns a random but structurally valid EVM key-value pair
// for use with the "evm" store. It selects uniformly among the four EVM key
// kinds, constructing keys and values that satisfy the length requirements
// enforced by flatKV's classifyAndPrefix / parse routines:
//
//   - Nonce:    key = 0x0a + addr(20 B),             value = 8 B
//   - CodeHash: key = 0x08 + addr(20 B),             value = 32 B
//   - Code:     key = 0x07 + addr(20 B),             value = 32 B (arbitrary)
//   - Storage:  key = 0x03 + addr(20 B) + slot(32 B), value = 32 B
func randomEVMKVPair(rng *testutil.TestRandom) *proto.KVPair {
	addr := randomTestBytes(rng, keys.AddressLen)
	switch rng.Intn(4) {
	case 0: // nonce
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: randomTestBytes(rng, 8)}
	case 1: // code hash
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: randomTestBytes(rng, 32)}
	case 2: // code
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr), Value: randomTestBytes(rng, 32)}
	default: // storage
		stripped := append(addr, randomTestBytes(rng, 32)...) // addr || slot
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyStorage, stripped), Value: randomTestBytes(rng, 32)}
	}
}

// randomEVMValue returns a random value of the correct length for the given EVM
// key (in x/evm store format). The key's leading prefix byte determines which
// value size is required.
func randomEVMValue(rng *testutil.TestRandom, key []byte) []byte {
	kind, _ := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyNonce:
		return randomTestBytes(rng, 8)
	case keys.EVMKeyCodeHash, keys.EVMKeyCode, keys.EVMKeyStorage:
		return randomTestBytes(rng, 32)
	default: // EVMKeyLegacy or unknown — no fixed constraint
		return randomTestBytes(rng, 8)
	}
}

// Perform random operations on the given database. After every block's
// ApplyChangeSets, commitFn is invoked so the caller can flush whichever
// underlying stores it cares about; pass a no-op (e.g. func() {}) for routers
// where commits are unnecessary or undesirable.
//
// All randomness is sourced from rng, so two calls with identical inputs and
// the same rng seed produce byte-identical apply / commit sequences.
func SimulateBlocks(
	t *testing.T,
	db Router,
	// Called once per block, immediately after ApplyChangeSets returns. Use this
	// to commit any underlying stores so subsequent reads / a subsequent restart
	// see the latest writes.
	commitFn func(),
	// Source of randomness. Reproducible from its seed.
	rng *testutil.TestRandom,
	// The keys currently in use for this simulation. Mutated by this method as
	// keys are inserted and deleted.
	keysInUse *liveKeySet,
	// Only keys with one of these store names will be created.
	stores []string,
	readsPerBlock int,
	updatesPerBlock int,
	deletesPerBlock int,
	newKeysPerBlock int,
	blocksToSimulate int,
) {
	t.Helper()

	for range blocksToSimulate {
		allPairs := make(map[string][]*proto.KVPair)

		// Insert brand-new keys distributed across the allowed stores.
		for range newKeysPerBlock {
			store := stores[rng.Intn(len(stores))]
			var pair *proto.KVPair
			if store == keys.EVMStoreKey {
				pair = randomEVMKVPair(rng)
			} else {
				pair = &proto.KVPair{Key: randomTestBytes(rng, 8), Value: randomTestBytes(rng, 8)}
			}
			allPairs[store] = append(allPairs[store], pair)
			keysInUse.Add(keyPair{store: store, key: string(pair.Key)})
		}

		// Overwrite existing keys with fresh values.
		for _, kp := range keysInUse.Sample(rng, updatesPerBlock) {
			var value []byte
			if kp.store == keys.EVMStoreKey {
				value = randomEVMValue(rng, []byte(kp.key))
			} else {
				value = randomTestBytes(rng, 8)
			}
			allPairs[kp.store] = append(allPairs[kp.store],
				&proto.KVPair{Key: []byte(kp.key), Value: value})
		}

		// Delete existing keys.
		toDelete := keysInUse.Sample(rng, deletesPerBlock)
		for _, kp := range toDelete {
			allPairs[kp.store] = append(allPairs[kp.store], &proto.KVPair{Key: []byte(kp.key), Delete: true})
		}

		// Iterate allPairs in deterministic store-name order so the changeset
		// slice handed to ApplyChangeSets is fully reproducible.
		storeNames := make([]string, 0, len(allPairs))
		for store := range allPairs {
			storeNames = append(storeNames, store)
		}
		sort.Strings(storeNames)
		cs := make([]*proto.NamedChangeSet, 0, len(allPairs))
		for _, store := range storeNames {
			cs = append(cs, &proto.NamedChangeSet{Name: store, Changeset: proto.ChangeSet{Pairs: allPairs[store]}})
		}
		require.NoError(t, db.ApplyChangeSets(cs, true), "ApplyChangeSets")
		for _, kp := range toDelete {
			keysInUse.Remove(kp)
		}
		commitFn()

		// Exercise the read path on a sample of existing keys.
		for _, kp := range keysInUse.Sample(rng, readsPerBlock) {
			_, _, err := db.Read(kp.store, []byte(kp.key))
			require.NoError(t, err, "Read store=%q key=%x", kp.store, kp.key)
		}
	}
}

// NewTestFlatKVCommitStore creates a [flatkv.CommitStore] rooted at dir.
// Pass a fresh t.TempDir() for a brand-new store; pass the same dir twice
// (after closing the first instance) to simulate a process restart.
// The latest committed version on disk is loaded. The returned store is
// automatically closed via t.Cleanup when the test finishes; close errors
// fail the test. flatkv.CommitStore.Close is idempotent, so callers may
// also Close the store explicitly (e.g. to simulate a restart) without
// causing the cleanup-time close to fail.
func NewTestFlatKVCommitStore(t *testing.T, dir string) *flatkv.CommitStore {
	t.Helper()
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = dir
	s, err := flatkv.NewCommitStore(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewTestFlatKVCommitStore: NewCommitStore: %v", err)
	}
	// LoadVersion(0, ...) loads the latest committed version on disk, or
	// initialises the store at version 0 if the directory is empty.
	if _, err := s.LoadVersion(0, false); err != nil {
		t.Fatalf("NewTestFlatKVCommitStore: LoadVersion: %v", err)
	}
	t.Cleanup(func() {
		require.NoError(t, s.Close(), "flatkv cleanup close (dir=%s)", dir)
	})
	return s
}

// NewTestMemIAVLCommitStore creates a [memiavl.CommitStore] rooted at dir,
// initialised with the given store names. Pass a fresh t.TempDir() for a
// brand-new store; pass the same dir twice (after closing the first instance)
// to simulate a process restart. The latest committed version on disk is
// loaded. The returned store is automatically closed via t.Cleanup when the
// test finishes; close errors fail the test. memiavl.CommitStore.Close is
// idempotent, so callers may also Close the store explicitly (e.g. to
// simulate a restart) without causing the cleanup-time close to fail.
func NewTestMemIAVLCommitStore(t *testing.T, dir string, storeNames []string) *memiavl.CommitStore {
	t.Helper()
	cs := memiavl.NewCommitStore(dir, memiavl.DefaultConfig())
	require.NoError(t, cs.Initialize(storeNames))
	if _, err := cs.LoadVersion(0, false); err != nil {
		t.Fatalf("NewTestMemIAVLCommitStore: LoadVersion: %v", err)
	}
	t.Cleanup(func() {
		require.NoError(t, cs.Close(), "memiavl cleanup close (dir=%s)", dir)
	})
	return cs
}
