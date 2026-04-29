package migration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
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
	if err := r.flatKV.ApplyChangeSets(changesets); err != nil {
		return err
	}
	_, err := r.flatKV.Commit()
	return err
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
	if err := r.memIAVL.ApplyChangeSets(changesets); err != nil {
		return err
	}
	_, err := r.memIAVL.Commit()
	return err
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

type keyPair struct {
	store string
	key   string
}

// randomTestBytes returns n random bytes suitable for use as a test key or value.
// Uses math/rand; cryptographic strength is not needed for test data generation.
func randomTestBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b) //nolint:gosec
	return b
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
func randomEVMKVPair() *proto.KVPair {
	addr := randomTestBytes(keys.AddressLen)
	switch rand.Intn(4) { //nolint:gosec
	case 0: // nonce
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: randomTestBytes(8)}
	case 1: // code hash
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: randomTestBytes(32)}
	case 2: // code
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr), Value: randomTestBytes(32)}
	default: // storage
		stripped := append(addr, randomTestBytes(32)...) // addr || slot
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyStorage, stripped), Value: randomTestBytes(32)}
	}
}

// randomEVMValue returns a random value of the correct length for the given EVM
// key (in x/evm store format). The key's leading prefix byte determines which
// value size is required.
func randomEVMValue(key []byte) []byte {
	kind, _ := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyNonce:
		return randomTestBytes(8)
	case keys.EVMKeyCodeHash, keys.EVMKeyCode, keys.EVMKeyStorage:
		return randomTestBytes(32)
	default: // EVMKeyLegacy or unknown — no fixed constraint
		return randomTestBytes(8)
	}
}

// sampleKeysInUse returns up to n (store, key) pairs drawn from keysInUse.
// Selection is random because Go randomises map iteration order on each
// traversal. If keysInUse contains fewer than n entries, all entries are
// returned. The returned slice is a snapshot; modifying it does not affect
// keysInUse.
func sampleKeysInUse(keysInUse map[keyPair]struct{}, n int) []keyPair {
	picked := make([]keyPair, 0, n)
	for kp := range keysInUse {
		picked = append(picked, kp)
		if len(picked) == n {
			break
		}
	}
	return picked
}

// Perform random operations on the given database.
func SimulateBlocks(
	t *testing.T,
	db Router,
	// The keys currently in use for this simulation. This map is updated by this method.
	keysInUse map[keyPair]struct{},
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
			store := stores[rand.Intn(len(stores))] //nolint:gosec
			var pair *proto.KVPair
			if store == keys.EVMStoreKey {
				pair = randomEVMKVPair()
			} else {
				pair = &proto.KVPair{Key: randomTestBytes(8), Value: randomTestBytes(8)}
			}
			allPairs[store] = append(allPairs[store], pair)
			keysInUse[keyPair{store: store, key: string(pair.Key)}] = struct{}{}
		}

		// Overwrite existing keys with fresh values.
		for _, kp := range sampleKeysInUse(keysInUse, updatesPerBlock) {
			var value []byte
			if kp.store == keys.EVMStoreKey {
				value = randomEVMValue([]byte(kp.key))
			} else {
				value = randomTestBytes(8)
			}
			allPairs[kp.store] = append(allPairs[kp.store],
				&proto.KVPair{Key: []byte(kp.key), Value: value})
		}

		// Delete existing keys.
		toDelete := sampleKeysInUse(keysInUse, deletesPerBlock)
		for _, kp := range toDelete {
			allPairs[kp.store] = append(allPairs[kp.store], &proto.KVPair{Key: []byte(kp.key), Delete: true})
		}

		cs := make([]*proto.NamedChangeSet, 0, len(allPairs))
		for store, pairs := range allPairs {
			cs = append(cs, &proto.NamedChangeSet{Name: store, Changeset: proto.ChangeSet{Pairs: pairs}})
		}
		require.NoError(t, db.ApplyChangeSets(ctx, cs), "ApplyChangeSets")
		for _, kp := range toDelete {
			delete(keysInUse, kp)
		}

		// Exercise the read path on a sample of existing keys.
		for _, kp := range sampleKeysInUse(keysInUse, readsPerBlock) {
			_, _, err := db.Read(kp.store, []byte(kp.key))
			require.NoError(t, err, "Read store=%q key=%x", kp.store, kp.key)
		}
	}
}

// NewTestFlatKVCommitStore creates a fresh [flatkv.CommitStore] backed by a
// temporary directory. The store is initialised at version 0 and is ready for
// use. It is automatically closed via t.Cleanup when the test finishes.
func NewTestFlatKVCommitStore(t *testing.T) *flatkv.CommitStore {
	t.Helper()
	cfg := flatkvconfig.DefaultTestConfig(t)
	s, err := flatkv.NewCommitStore(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewTestFlatKVCommitStore: NewCommitStore: %v", err)
	}
	if _, err := s.LoadVersion(0, false); err != nil {
		t.Fatalf("NewTestFlatKVCommitStore: LoadVersion: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// NewTestMemIAVLCommitStore creates a fresh [memiavl.CommitStore] backed by a
// temporary directory, initialised with the given store names and loaded at
// version 0. It is ready for use and is automatically closed via t.Cleanup
// when the test finishes.
func NewTestMemIAVLCommitStore(t *testing.T, storeNames []string) *memiavl.CommitStore {
	t.Helper()
	cs := memiavl.NewCommitStore(t.TempDir(), memiavl.DefaultConfig())
	cs.Initialize(storeNames)
	if _, err := cs.LoadVersion(0, false); err != nil {
		t.Fatalf("NewTestMemIAVLCommitStore: LoadVersion: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
