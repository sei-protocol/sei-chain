package migration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// migrationTestSeedEnvVar is the environment variable used to override the seed
// of a migration test. Setting it to the value logged by a previously failing
// test reproduces the exact same execution.
const migrationTestSeedEnvVar = "MIGRATION_TEST_SEED"

// newSeededTestRandom returns a *utils.TestRandom whose seed comes from the
// MIGRATION_TEST_SEED env var when set, or the current Unix nanos otherwise.
// The seed is logged via t.Logf as a shell-pasteable assignment so that copying
// the line and prefixing "go test ..." reproduces a failure exactly.
func newSeededTestRandom(t *testing.T) *utils.TestRandom {
	t.Helper()
	seed := time.Now().UnixNano()
	if s := os.Getenv(migrationTestSeedEnvVar); s != "" {
		parsed, err := strconv.ParseInt(s, 10, 64)
		require.NoError(t, err, "invalid %s", migrationTestSeedEnvVar)
		seed = parsed
	}
	rng := utils.NewTestRandomNoPrint(seed)
	t.Logf("%s=%d", migrationTestSeedEnvVar, rng.SeedValue())
	return rng
}

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

func (m *TestMultiDB) ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error {
	for _, nestedDB := range m.nestedDBs {
		err := nestedDB.ApplyChangeSets(ctx, changesets)
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

func (m *TestMultiDB) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	// The multi-DB utility does not support testing of iteration.
	panic("unimplemented")
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

func (r *TestFlatKVRouter) ApplyChangeSets(_ context.Context, changesets []*proto.NamedChangeSet) error {
	return r.flatKV.ApplyChangeSets(changesets)
}

func (r *TestFlatKVRouter) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	return nil, errors.New("TestFlatKVRouter does not support iteration")
}

func (r *TestFlatKVRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestFlatKVRouter does not support proofs")
}

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

func (r *TestMemIAVLRouter) ApplyChangeSets(_ context.Context, changesets []*proto.NamedChangeSet) error {
	return r.memIAVL.ApplyChangeSets(changesets)
}

func (r *TestMemIAVLRouter) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	return nil, errors.New("TestMemIAVLRouter does not support iteration")
}

func (r *TestMemIAVLRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestMemIAVLRouter does not support proofs")
}

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

func (r *TestInMemoryRouter) ApplyChangeSets(_ context.Context, changesets []*proto.NamedChangeSet) error {
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

func (r *TestInMemoryRouter) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	return nil, errors.New("TestInMemoryRouter does not support iteration")
}

func (r *TestInMemoryRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestInMemoryRouter does not support proofs")
}

// Get a random store-key pair. Returns ok=false if the database is empty.
// Relies on Go's randomized map iteration order; the returned key is a
// fresh copy that is safe for the caller to mutate.
func (r *TestInMemoryRouter) RandomKey() (store string, key []byte, ok bool) {
	for storeName, storeMap := range r.stores {
		for k := range storeMap {
			return storeName, []byte(k), true
		}
	}
	return "", nil, false
}

// Returns two slices: one with store names, one with keys. The slices will be the same size, and the store at
// each index corresponds to the key at the same index in the other slice.
func (r *TestInMemoryRouter) GetAllKeys() (stores []string, keys [][]byte) {
	total := 0
	for _, storeMap := range r.stores {
		total += len(storeMap)
	}
	stores = make([]string, 0, total)
	keys = make([][]byte, 0, total)
	for storeName, storeMap := range r.stores {
		for k := range storeMap {
			stores = append(stores, storeName)
			keys = append(keys, []byte(k))
		}
	}
	return stores, keys
}

// ToChangeSets returns the current state as a batch of [proto.NamedChangeSet]s
// suitable for bulk-loading a fresh router to the same logical state. All
// key-value pairs from every known store are included; deleted keys are not (they
// have already been removed from the in-memory map).
func (r *TestInMemoryRouter) ToChangeSets() []*proto.NamedChangeSet {
	cs := make([]*proto.NamedChangeSet, 0, len(r.stores))
	for storeName, storeMap := range r.stores {
		pairs := make([]*proto.KVPair, 0, len(storeMap))
		for k, v := range storeMap {
			valCopy := append([]byte(nil), v...)
			pairs = append(pairs, &proto.KVPair{Key: []byte(k), Value: valCopy})
		}
		if len(pairs) > 0 {
			cs = append(cs, &proto.NamedChangeSet{
				Name:      storeName,
				Changeset: proto.ChangeSet{Pairs: pairs},
			})
		}
	}
	return cs
}

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
	count, err := flatkv.CountKeys(flatKV)
	require.NoError(t, err)
	return count
}

// GetMemIAVLKeyCount returns the total number of keys stored across every tree
// in the given memIAVL CommitStore.
func GetMemIAVLKeyCount(t *testing.T, memIAVL *memiavl.CommitStore) int64 {
	t.Helper()
	var total int64
	for _, namedTree := range memIAVL.GetDB().Trees() {
		iter := namedTree.Tree.Iterator(nil, nil, true)
		for ; iter.Valid(); iter.Next() {
			total++
		}
		require.NoError(t, iter.Error(), "iterator error on tree %q", namedTree.Name)
		_ = iter.Close()
	}
	return total
}

// GetMemIAVLStoreHashes returns a map of store name → committed root hash for
// every tree in the given memIAVL CommitStore. The CommitID version is
// intentionally omitted so callers can compare hashes across instances that
// were built with different numbers of blocks.
func GetMemIAVLStoreHashes(t *testing.T, memIAVL *memiavl.CommitStore) map[string][]byte {
	t.Helper()
	info := memIAVL.LastCommitInfo()
	require.NotNil(t, info, "LastCommitInfo returned nil")
	hashes := make(map[string][]byte, len(info.StoreInfos))
	for _, si := range info.StoreInfos {
		hashes[si.Name] = append([]byte(nil), si.CommitId.Hash...)
	}
	return hashes
}

// ReadMigrationVersionFromFlatKV reads the stored migration version from flatKV's
// MigrationStore. Returns (version, true) if the key is present, (0, false) if absent.
func ReadMigrationVersionFromFlatKV(t *testing.T, flatKV *flatkv.CommitStore) (uint64, bool) {
	t.Helper()
	v, ok, err := readVersionFromDB(buildFlatKVReader(flatKV))
	require.NoError(t, err, "ReadMigrationVersionFromFlatKV")
	return v, ok
}

// ReadMigrationVersionFromMemIAVL reads the stored migration version from memiavl's
// MigrationStore. Returns (version, true) if the key is present, (0, false) if absent.
func ReadMigrationVersionFromMemIAVL(t *testing.T, memIAVL *memiavl.CommitStore) (uint64, bool) {
	t.Helper()
	v, ok, err := readVersionFromDB(buildMemIAVLReader(memIAVL))
	require.NoError(t, err, "ReadMigrationVersionFromMemIAVL")
	return v, ok
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

func (s *liveKeySet) Has(kp keyPair) bool {
	_, ok := s.idx[kp]
	return ok
}

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
func (s *liveKeySet) Sample(r *utils.TestRandom, n int) []keyPair {
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
// Uses the supplied *utils.TestRandom so output is deterministic given a seed.
func randomTestBytes(rng *utils.TestRandom, n int) []byte {
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
func randomEVMKVPair(rng *utils.TestRandom) *proto.KVPair {
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
func randomEVMValue(rng *utils.TestRandom, key []byte) []byte {
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
	rng *utils.TestRandom,
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
	ctx := context.Background()

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
		require.NoError(t, db.ApplyChangeSets(ctx, cs), "ApplyChangeSets")
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
// automatically closed via t.Cleanup when the test finishes; explicit
// Close calls before then are safe.
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
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// NewTestMemIAVLCommitStore creates a [memiavl.CommitStore] rooted at dir,
// initialised with the given store names. Pass a fresh t.TempDir() for a
// brand-new store; pass the same dir twice (after closing the first instance)
// to simulate a process restart. The latest committed version on disk is
// loaded. The returned store is automatically closed via t.Cleanup when the
// test finishes; explicit Close calls before then are safe.
func NewTestMemIAVLCommitStore(t *testing.T, dir string, storeNames []string) *memiavl.CommitStore {
	t.Helper()
	cs := memiavl.NewCommitStore(dir, memiavl.DefaultConfig())
	cs.Initialize(storeNames)
	if _, err := cs.LoadVersion(0, false); err != nil {
		t.Fatalf("NewTestMemIAVLCommitStore: LoadVersion: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
