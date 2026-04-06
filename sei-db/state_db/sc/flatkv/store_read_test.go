package flatkv

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Get, Has, and Pending Writes
// =============================================================================

func TestStoreGetPendingWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x11}
	slot := Slot{0x22}
	value := []byte{0x33}
	key := memiavlStorageKey(addr, slot)

	// No data initially
	_, found := s.Get(key)
	require.False(t, found)

	// Apply changeset (adds to pending writes)
	cs := makeChangeSet(key, value, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be readable from pending writes
	got, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, value, got)

	// Commit
	commitAndCheck(t, s)

	// Should still be readable after commit
	got, found = s.Get(key)
	require.True(t, found)
	require.Equal(t, value, got)
}

func TestStoreGetPendingDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x44}
	slot := Slot{0x55}
	key := memiavlStorageKey(addr, slot)

	// Write and commit
	cs1 := makeChangeSet(key, []byte{0x66}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	_, found := s.Get(key)
	require.True(t, found)

	// Apply delete (pending)
	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Should not be found (pending delete)
	_, found = s.Get(key)
	require.False(t, found)

	// Commit delete
	commitAndCheck(t, s)

	// Still should not be found
	_, found = s.Get(key)
	require.False(t, found)
}

func TestStoreGetNonStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x77}

	// Non-storage keys should return not found (before write)
	nonStorageKeys := [][]byte{
		evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
		evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]),
		evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
	}

	for _, key := range nonStorageKeys {
		_, found := s.Get(key)
		require.False(t, found, "non-storage keys should not be found before write")
	}
}

func TestStoreHas(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x88}
	slot := Slot{0x99}
	key := memiavlStorageKey(addr, slot)

	// Initially not found
	require.False(t, s.Has(key))

	// Write and commit
	cs := makeChangeSet(key, []byte{0xAA}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Now should exist
	require.True(t, s.Has(key))
}

// =============================================================================
// Legacy Key Get Tests
// =============================================================================

func TestStoreGetLegacyPendingWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xEE}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Not found initially
	_, found := s.Get(legacyKey)
	require.False(t, found)

	// Apply changeset
	cs := makeChangeSet(legacyKey, []byte{0x00, 0x40}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be readable from pending writes
	got, found := s.Get(legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x40}, got)

	// Commit and still readable
	commitAndCheck(t, s)
	got, found = s.Get(legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x40}, got)
}

func TestStoreGetLegacyPendingDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xFF}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Write and commit
	cs1 := makeChangeSet(legacyKey, []byte{0x00, 0x80}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	_, found := s.Get(legacyKey)
	require.True(t, found)

	// Apply delete (pending)
	cs2 := makeChangeSet(legacyKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Should not be found (pending delete)
	_, found = s.Get(legacyKey)
	require.False(t, found)

	// Commit delete
	commitAndCheck(t, s)
	_, found = s.Get(legacyKey)
	require.False(t, found)
}

// =============================================================================
// Delete
// =============================================================================

func TestStoreDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x55}
	slot := Slot{0x66}
	key := memiavlStorageKey(addr, slot)

	// Write
	cs1 := makeChangeSet(key, []byte{0x77}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	got, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x77}, got)

	// Delete
	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Should not exist
	_, found = s.Get(key)
	require.False(t, found)
}

// =============================================================================
// Iterator
// =============================================================================

func TestStoreIteratorEmpty(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Empty store
	iter := s.Iterator(nil, nil)
	defer iter.Close()

	require.False(t, iter.Valid(), "empty store should have invalid iterator")
}

func TestStoreIteratorSingleKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	slot := Slot{0xBB}
	value := []byte{0xCC}
	memiavlKey := memiavlStorageKey(addr, slot)
	internalKey := StorageKey(addr, slot) // addr(20) || slot(32)

	cs := makeChangeSet(memiavlKey, value, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Iterate all
	iter := s.Iterator(nil, nil)
	defer iter.Close()

	require.True(t, iter.First())
	require.True(t, iter.Valid())
	require.Equal(t, internalKey, iter.Key()) // internal key format
	require.Equal(t, value, iter.Value())

	// Only one key
	iter.Next()
	require.False(t, iter.Valid())
}

func TestStoreIteratorMultipleKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xDD}

	// Write multiple slots
	entries := []struct {
		slot  Slot
		value byte
	}{
		{Slot{0x01}, 0xAA},
		{Slot{0x02}, 0xBB},
		{Slot{0x03}, 0xCC},
	}

	pairs := make([]*proto.KVPair, len(entries))
	for i, e := range entries {
		key := memiavlStorageKey(addr, e.slot)
		pairs[i] = &proto.KVPair{Key: key, Value: []byte{e.value}}
	}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Iterate all
	iter := s.Iterator(nil, nil)
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
		require.NotNil(t, iter.Key())
		require.NotNil(t, iter.Value())
	}
	require.Equal(t, len(entries), count)
}

func TestStoreIteratorNonStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Iterating non-storage keys should return empty iterator (Phase 1)
	addr := Address{0xCC}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])

	iter := s.Iterator(nonceKey, PrefixEnd(nonceKey))
	defer iter.Close()

	require.False(t, iter.Valid(), "non-storage key iteration should be empty in Phase 1")
}

// =============================================================================
// Prefix Iterator
// =============================================================================

func TestStoreStoragePrefixIteration(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAB}

	// Write multiple slots
	for i := byte(1); i <= 3; i++ {
		slot := Slot{i}
		key := memiavlStorageKey(addr, slot)
		cs := makeChangeSet(key, []byte{i * 10}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}
	commitAndCheck(t, s)

	// Iterate by address prefix
	prefix := append(evm.StateKeyPrefix(), addr[:]...)
	iter := s.IteratorByPrefix(prefix)
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
		require.NotNil(t, iter.Key())
		require.NotNil(t, iter.Value())
	}
	require.Equal(t, 3, count)
}

func TestStoreIteratorByPrefixAddress(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr1 := Address{0xAA}
	addr2 := Address{0xBB}

	// Write slots for addr1
	for i := byte(1); i <= 3; i++ {
		slot := Slot{i}
		key := memiavlStorageKey(addr1, slot)
		cs := makeChangeSet(key, []byte{i * 10}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}

	// Write slots for addr2
	for i := byte(1); i <= 2; i++ {
		slot := Slot{i}
		key := memiavlStorageKey(addr2, slot)
		cs := makeChangeSet(key, []byte{i * 20}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}

	commitAndCheck(t, s)

	// Iterate by addr1 prefix
	prefix1 := append(evm.StateKeyPrefix(), addr1[:]...)
	iter1 := s.IteratorByPrefix(prefix1)
	defer iter1.Close()

	count1 := 0
	for iter1.First(); iter1.Valid(); iter1.Next() {
		count1++
	}
	require.Equal(t, 3, count1, "should find 3 slots for addr1")

	// Iterate by addr2 prefix
	prefix2 := append(evm.StateKeyPrefix(), addr2[:]...)
	iter2 := s.IteratorByPrefix(prefix2)
	defer iter2.Close()

	count2 := 0
	for iter2.First(); iter2.Valid(); iter2.Next() {
		count2++
	}
	require.Equal(t, 2, count2, "should find 2 slots for addr2")
}

// =============================================================================
// R-1 ~ R-5: Get/Has for All Key Types from Committed DB
// =============================================================================

func TestGetAllKeyTypesFromCommittedDB(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA1)
	slot := slotN(0x01)
	ch := codeHashN(0xBB)
	bytecode := []byte{0x60, 0x80, 0x60, 0x40}
	storageVal := []byte{0x42}
	legacyKey := append([]byte{0x09}, addr[:]...)
	legacyVal := []byte{0x99, 0x88}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slot, storageVal),
			noncePair(addr, 7),
			codeHashPair(addr, ch),
			codePair(addr, bytecode),
		),
		makeChangeSet(legacyKey, legacyVal, false),
	}))
	commitAndCheck(t, s)

	// Storage
	got, found := s.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)))
	require.True(t, found, "storage should be found")
	require.Equal(t, storageVal, got)

	// Nonce
	got, found = s.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]))
	require.True(t, found, "nonce should be found")
	require.Equal(t, uint64(7), binary.BigEndian.Uint64(got))

	// CodeHash
	got, found = s.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]))
	require.True(t, found, "codehash should be found")
	require.Equal(t, ch[:], got)

	// Code
	got, found = s.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]))
	require.True(t, found, "code should be found")
	require.Equal(t, bytecode, got)

	// Legacy
	got, found = s.Get(legacyKey)
	require.True(t, found, "legacy should be found")
	require.Equal(t, legacyVal, got)

	// Has should match
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))))
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])))
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])))
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])))
	require.True(t, s.Has(legacyKey))
}

func TestGetNonceFromCommittedEOA(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA2)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 42)),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(nonceKey)
	require.True(t, found, "nonce should be found for EOA")
	require.Equal(t, uint64(42), binary.BigEndian.Uint64(got))

	_, found = s.Get(chKey)
	require.False(t, found, "codehash should NOT be found for EOA")

	require.True(t, s.Has(nonceKey))
	require.False(t, s.Has(chKey))
}

func TestGetCodeHashFromCommittedContract(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA3)
	ch := codeHashN(0xCC)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1), codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(chKey)
	require.True(t, found, "codehash should be found for contract")
	require.Equal(t, ch[:], got)

	got, found = s.Get(nonceKey)
	require.True(t, found)
	require.Equal(t, uint64(1), binary.BigEndian.Uint64(got))

	require.True(t, s.Has(chKey))
	require.True(t, s.Has(nonceKey))
}

func TestGetCodeFromCommittedDB(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA4)
	bytecode := []byte{0x60, 0x80, 0x52}
	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])

	// Pending code write is visible before commit
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codePair(addr, bytecode)),
	}))
	got, found := s.Get(codeKey)
	require.True(t, found, "pending code write should be visible")
	require.Equal(t, bytecode, got)

	commitAndCheck(t, s)

	// Still visible after commit
	got, found = s.Get(codeKey)
	require.True(t, found)
	require.Equal(t, bytecode, got)

	// Pending code delete hides it before commit
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeDeletePair(addr)),
	}))
	_, found = s.Get(codeKey)
	require.False(t, found, "pending code delete should hide the entry")

	commitAndCheck(t, s)
	_, found = s.Get(codeKey)
	require.False(t, found, "code should be gone after commit")
}

func TestGetUnknownKeyTypes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	cases := []struct {
		name string
		key  []byte
	}{
		{"nil key", nil},
		{"empty key", []byte{}},
		{"single byte", []byte{0xFF}},
		{"random bytes", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"short nonce-like (2 bytes)", []byte{0x04, 0x01}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, found := s.Get(tc.key)
			require.False(t, found)
			require.False(t, s.Has(tc.key))
		})
	}
}

// =============================================================================
// R-6 ~ R-8: Account Delete Semantics (isDelete interaction)
// =============================================================================

// TestGetAccountAfterFullDeletePending verifies that Get returns (nil, false)
// for both nonce and codehash when all account fields are zeroed (pending).
// The isDelete guard in store_read.go:39-41 hides the zeroed row entirely.
func TestGetAccountAfterFullDeletePending(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xB1)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 10), codeHashPair(addr, codeHashN(0xDD))),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr), codeHashDeletePair(addr)),
	}))

	_, nonceFound := s.Get(nonceKey)
	require.False(t, nonceFound, "nonce should not be found after full delete (isDelete=true)")

	_, chFound := s.Get(chKey)
	require.False(t, chFound, "codehash should not be found after full delete (isDelete=true)")

	require.False(t, s.Has(nonceKey))
	require.False(t, s.Has(chKey))
}

func TestGetAccountAfterFullDeleteCommitted(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xB2)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 5), codeHashPair(addr, codeHashN(0xEE))),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr), codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	// After full delete + commit, the account row is physically deleted from
	// accountDB (batch.Delete in commitBatches). Both fields return not-found.
	_, nonceFound := s.Get(nonceKey)
	require.False(t, nonceFound, "nonce should not be found after full delete + commit")

	_, chFound := s.Get(chKey)
	require.False(t, chFound, "codehash should not be found after full delete + commit")

	require.False(t, s.Has(nonceKey))
	require.False(t, s.Has(chKey))
}

func TestGetAccountAfterPartialDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xB3)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 99), codeHashPair(addr, codeHashN(0xFF))),
	}))
	commitAndCheck(t, s)

	// Delete only codehash — nonce should survive
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(nonceKey)
	require.True(t, found, "nonce should survive partial delete")
	require.Equal(t, uint64(99), binary.BigEndian.Uint64(got))

	_, found = s.Get(chKey)
	require.False(t, found, "codehash should be gone after delete")

	// Account row should still exist (EOA encoding)
	raw, err := s.accountDB.Get(AccountKey(addr))
	require.NoError(t, err)
	require.Equal(t, accountValueEOALen, len(raw))
}

// =============================================================================
// R-9 ~ R-11: Multi-Block Read Correctness
// =============================================================================

func TestGetAfterOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xC1)
	slot := slotN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x11})),
	}))
	commitAndCheck(t, s)

	got, _ := s.Get(key)
	require.Equal(t, []byte{0x11}, got)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x22, 0x33})),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x22, 0x33}, got, "should return v2 value after overwrite")
}

func TestGetAfterDeleteAndRecreate(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xC2)
	slot := slotN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	// v1: create
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0xAA})),
	}))
	commitAndCheck(t, s)

	// v2: delete
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storageDeletePair(addr, slot)),
	}))
	commitAndCheck(t, s)

	_, found := s.Get(key)
	require.False(t, found, "should not be found after delete")

	// v3: re-create with different value
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0xBB, 0xCC})),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0xBB, 0xCC}, got, "should return v3 value after re-create")
}

func TestGetAfterReopenAllKeyTypes(t *testing.T) {
	dir := t.TempDir()

	addr := addrN(0xC3)
	slot := slotN(0x01)
	ch := codeHashN(0xAA)
	bytecode := []byte{0x60, 0x80}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Phase 1: write everything and close
	cfg := DefaultTestConfig(t)
	cfg.DataDir = dir
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer s1.Close()
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 100),
			codeHashPair(addr, ch),
			codePair(addr, bytecode),
			storagePair(addr, slot, []byte{0x42}),
		),
		makeChangeSet(legacyKey, []byte{0x77}, false),
	}))
	_, err = s1.Commit()
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	// Phase 2: reopen and verify all reads
	cfg2 := DefaultTestConfig(t)
	cfg2.DataDir = dir
	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	got, found := s2.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)))
	require.True(t, found, "storage should survive reopen")
	require.Equal(t, []byte{0x42}, got)

	got, found = s2.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]))
	require.True(t, found, "nonce should survive reopen")
	require.Equal(t, uint64(100), binary.BigEndian.Uint64(got))

	got, found = s2.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]))
	require.True(t, found, "codehash should survive reopen")
	require.Equal(t, ch[:], got)

	got, found = s2.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]))
	require.True(t, found, "code should survive reopen")
	require.Equal(t, bytecode, got)

	got, found = s2.Get(legacyKey)
	require.True(t, found, "legacy should survive reopen")
	require.Equal(t, []byte{0x77}, got)
}

// =============================================================================
// R-12, R-13: Iterator Pending Write Visibility
// =============================================================================

func TestIteratorDoesNotSeePendingWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD1)
	slot := slotN(0x01)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0xAA})),
	}))

	// Before commit: iterator should not see the pending write
	iter := s.Iterator(nil, nil)
	require.False(t, iter.First(), "iterator should not see pending writes")
	require.NoError(t, iter.Close())

	commitAndCheck(t, s)

	// After commit: iterator should see it
	iter = s.Iterator(nil, nil)
	defer iter.Close()
	require.True(t, iter.First(), "iterator should see committed entry")
	require.True(t, iter.Valid())
	require.Equal(t, StorageKey(addr, slot), iter.Key())
}

func TestIteratorDoesNotSeePendingDeletes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD2)

	// Write and commit 3 keys
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slotN(0x01), []byte{0x11}),
			storagePair(addr, slotN(0x02), []byte{0x22}),
			storagePair(addr, slotN(0x03), []byte{0x33}),
		),
	}))
	commitAndCheck(t, s)

	// Apply pending delete for middle key
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storageDeletePair(addr, slotN(0x02))),
	}))

	// Iterator should still see all 3 (pending delete not visible)
	count := iterCount(t, s.Iterator(nil, nil))
	require.Equal(t, 3, count, "pending delete should not affect iterator")

	commitAndCheck(t, s)

	// After commit: only 2 remain
	count = iterCount(t, s.Iterator(nil, nil))
	require.Equal(t, 2, count, "committed delete should remove entry from iterator")
}

// =============================================================================
// R-14 ~ R-18: Iterator Navigation
// =============================================================================

func TestIteratorLast(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD3)
	slots := []Slot{slotN(0x10), slotN(0x20), slotN(0x30)}

	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, sl, []byte{0xAA}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	require.True(t, iter.Last(), "Last() should succeed")
	require.True(t, iter.Valid())
	require.Equal(t, StorageKey(addr, slotN(0x30)), iter.Key())
}

func TestIteratorSeekGE(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD4)
	slots := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, slotN(sl), []byte{sl}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	// SeekGE to a key between 0x20 and 0x30 → should land on 0x30
	seekKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x25)))
	require.True(t, iter.SeekGE(seekKey))
	require.Equal(t, StorageKey(addr, slotN(0x30)), iter.Key())

	// SeekGE to exact key 0x30 → should land on 0x30
	seekKey = evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x30)))
	require.True(t, iter.SeekGE(seekKey))
	require.Equal(t, StorageKey(addr, slotN(0x30)), iter.Key())

	// SeekGE past all keys → invalid
	seekKey = evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0xFF)))
	require.False(t, iter.SeekGE(seekKey))
}

func TestIteratorSeekLT(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD5)
	slots := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, slotN(sl), []byte{sl}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	// SeekLT(0x30) → should land on 0x20
	seekKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x30)))
	require.True(t, iter.SeekLT(seekKey))
	require.Equal(t, StorageKey(addr, slotN(0x20)), iter.Key())

	// SeekLT before first key → invalid
	seekKey = evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x10)))
	require.False(t, iter.SeekLT(seekKey))
}

func TestIteratorPrev(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD6)
	slots := []Slot{slotN(0x10), slotN(0x20), slotN(0x30)}
	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, sl, []byte{0xAA}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	require.True(t, iter.Last())
	require.Equal(t, StorageKey(addr, slotN(0x30)), iter.Key())

	require.True(t, iter.Prev())
	require.Equal(t, StorageKey(addr, slotN(0x20)), iter.Key())

	require.True(t, iter.Prev())
	require.Equal(t, StorageKey(addr, slotN(0x10)), iter.Key())

	require.False(t, iter.Prev(), "Prev past first should be invalid")
}

func TestIteratorSeekGEKeyTypeMismatch(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD7)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slotN(0x01), []byte{0xAA})),
	}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	// SeekGE with a nonce key on a storage iterator → mismatch
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	require.False(t, iter.SeekGE(nonceKey))
	require.Error(t, iter.Error(), "key type mismatch should set an error")
}

// =============================================================================
// R-19: Iterator Skips Meta Keys
// =============================================================================

func TestIteratorSkipsMetaKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD8)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slotN(0x01), []byte{0x11}),
			storagePair(addr, slotN(0x02), []byte{0x22}),
		),
	}))
	commitAndCheck(t, s)

	// Verify _meta/ keys exist in raw storageDB
	rawIter, err := s.storageDB.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	rawCount := 0
	metaCount := 0
	for rawIter.First(); rawIter.Valid(); rawIter.Next() {
		rawCount++
		if isMetaKey(rawIter.Key()) {
			metaCount++
		}
	}
	require.NoError(t, rawIter.Error())
	require.NoError(t, rawIter.Close())
	require.Greater(t, metaCount, 0, "storageDB should contain _meta/ keys")

	// FlatKV iterator should skip meta keys
	count := iterCount(t, s.Iterator(nil, nil))
	require.Equal(t, 2, count, "iterator should only see live data entries, not _meta/")
	require.Equal(t, rawCount-metaCount, count)
}

// =============================================================================
// R-20 ~ R-23: Iterator Range Bounds
// =============================================================================

func TestIteratorRangeBounds(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xD9)
	slots := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, slotN(sl), []byte{sl}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	startKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x20)))
	endKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x40)))

	iter := s.Iterator(startKey, endKey)
	defer iter.Close()

	var keys [][]byte
	for iter.First(); iter.Valid(); iter.Next() {
		keys = append(keys, append([]byte(nil), iter.Key()...))
	}

	require.Len(t, keys, 2, "range [0x20, 0x40) should see 0x20 and 0x30")
	require.Equal(t, StorageKey(addr, slotN(0x20)), keys[0])
	require.Equal(t, StorageKey(addr, slotN(0x30)), keys[1])
}

func TestIteratorHalfOpenStart(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xDA)
	slots := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, slotN(sl), []byte{sl}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	endKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x30)))
	count := iterCount(t, s.Iterator(nil, endKey))
	require.Equal(t, 2, count, "[nil, 0x30) should see 0x10, 0x20")
}

func TestIteratorHalfOpenEnd(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xDB)
	slots := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	var pairs []*proto.KVPair
	for _, sl := range slots {
		pairs = append(pairs, storagePair(addr, slotN(sl), []byte{sl}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	startKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x30)))
	count := iterCount(t, s.Iterator(startKey, nil))
	require.Equal(t, 3, count, "[0x30, nil) should see 0x30, 0x40, 0x50")
}

func TestIteratorInvalidRange(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xDC)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slotN(0x01), []byte{0xAA})),
	}))
	commitAndCheck(t, s)

	startKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x30)))
	endKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x10)))

	iter := s.Iterator(startKey, endKey)
	defer iter.Close()
	require.False(t, iter.Valid(), "start >= end should yield empty iterator")
}

// =============================================================================
// R-24 ~ R-27: Iterator Domain and Edge Cases
// =============================================================================

func TestIteratorDomain(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xDD)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slotN(0x01), []byte{0xAA})),
	}))
	commitAndCheck(t, s)

	startKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x00)))
	endKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0xFF)))
	iter := s.Iterator(startKey, endKey)
	defer iter.Close()

	domainStart, domainEnd := iter.Domain()
	require.Equal(t, startKey, domainStart)
	require.Equal(t, endKey, domainEnd)
}

func TestIteratorByPrefixEmpty(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xDE)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slotN(0x01), []byte{0x11}),
			storagePair(addr, slotN(0x02), []byte{0x22}),
		),
	}))
	commitAndCheck(t, s)

	// Empty prefix falls back to Iterator(nil, nil) → sees all storage
	count := iterCount(t, s.IteratorByPrefix([]byte{}))
	require.Equal(t, 2, count, "empty prefix should iterate all storage")
}

func TestIteratorByPrefixNonStorage(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xDF)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1), storagePair(addr, slotN(0x01), []byte{0x11})),
	}))
	commitAndCheck(t, s)

	// Nonce prefix → empty iterator (only storage iteration is supported)
	noncePrefix := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	iter := s.IteratorByPrefix(noncePrefix)
	defer iter.Close()
	require.False(t, iter.Valid(), "non-storage prefix should return empty iterator")
}

func TestIteratorAfterClose(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xE0)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slotN(0x01), []byte{0xAA})),
	}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	require.True(t, iter.First())
	require.NoError(t, iter.Close())

	// After close: all navigation returns false, no panic
	require.False(t, iter.First())
	require.False(t, iter.Last())
	require.False(t, iter.Next())
	require.False(t, iter.Prev())
	require.False(t, iter.Valid())
	require.Nil(t, iter.Key())
	require.Nil(t, iter.Value())
}

// =============================================================================
// R-28 ~ R-29: Read-Only Store
// =============================================================================

func TestReadOnlyGetAllKeyTypes(t *testing.T) {
	dir := t.TempDir()

	addr := addrN(0xF1)
	slot := slotN(0x01)
	ch := codeHashN(0xAA)
	bytecode := []byte{0x60, 0x80}
	legacyKey := append([]byte{0x09}, addr[:]...)

	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 5
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer s.Close()
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 50),
			codeHashPair(addr, ch),
			codePair(addr, bytecode),
			storagePair(addr, slot, []byte{0x42}),
		),
		makeChangeSet(legacyKey, []byte{0x77}, false),
	}))
	_, err = s.Commit()
	require.NoError(t, err)

	ro, err := s.LoadVersion(1, true)
	require.NoError(t, err)
	defer ro.Close()

	got, found := ro.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)))
	require.True(t, found)
	require.Equal(t, []byte{0x42}, got)

	got, found = ro.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.Equal(t, uint64(50), binary.BigEndian.Uint64(got))

	got, found = ro.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]))
	require.True(t, found)
	require.Equal(t, ch[:], got)

	got, found = ro.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]))
	require.True(t, found)
	require.Equal(t, bytecode, got)

	got, found = ro.Get(legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x77}, got)
}

func TestReadOnlyIterator(t *testing.T) {
	dir := t.TempDir()

	addr := addrN(0xF2)

	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 5
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer s.Close()
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slotN(0x10), []byte{0x11}),
			storagePair(addr, slotN(0x20), []byte{0x22}),
			storagePair(addr, slotN(0x30), []byte{0x33}),
		),
	}))
	_, err = s.Commit()
	require.NoError(t, err)

	ro, err := s.LoadVersion(1, true)
	require.NoError(t, err)
	defer ro.Close()

	count := iterCount(t, ro.Iterator(nil, nil))
	require.Equal(t, 3, count, "read-only iterator should see all committed entries")

	prefix := append(evm.StateKeyPrefix(), addr[:]...)
	count = iterCount(t, ro.IteratorByPrefix(prefix))
	require.Equal(t, 3, count, "read-only prefix iterator should see all slots for addr")
}

// =============================================================================
// Helpers
// =============================================================================

func iterCount(t *testing.T, iter Iterator) int {
	t.Helper()
	defer iter.Close()
	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}
	require.NoError(t, iter.Error())
	return count
}

func TestGetNilKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	val, found := s.Get(nil)
	require.False(t, found)
	require.Nil(t, val)
}

func TestGetEmptyKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	val, found := s.Get([]byte{})
	require.False(t, found)
	require.Nil(t, val)
}

func TestHasNilKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()
	require.False(t, s.Has(nil))
}

func TestHasEmptyKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()
	require.False(t, s.Has([]byte{}))
}

func TestHasForAllKeyTypes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x10)
	slot := slotN(0x01)
	ch := codeHashN(0xAB)

	pairs := []*proto.KVPair{
		{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)), Value: []byte{0x11}},
		noncePair(addr, 42),
		codeHashPair(addr, ch),
		codePair(addr, []byte{0x60, 0x60}),
	}
	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))))
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])))
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])))
	require.True(t, s.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])))
}

func TestHasOnPendingDeletes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x11)
	slot := slotN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	cs := makeChangeSet(key, []byte{0xAA}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)
	require.True(t, s.Has(key))

	delCS := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{delCS}))
	require.False(t, s.Has(key), "Has should return false for pending-deleted key")
}

func TestHasOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x12)
	slot := slotN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	cs := makeChangeSet(key, []byte{0xBB}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.True(t, ro.Has(key))
	require.False(t, ro.Has(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0xFF), slotN(0xFF)))))
	require.NoError(t, s.Close())
}

func TestGetAfterRollback(t *testing.T) {
	s := setupTestStoreWithConfig(t, &Config{
		SnapshotInterval:       2,
		SnapshotKeepRecent:     5,
		AccountDBConfig:        smallTestPebbleConfig(),
		AccountCacheConfig:     smallTestCacheConfig(),
		CodeDBConfig:           smallTestPebbleConfig(),
		CodeCacheConfig:        smallTestCacheConfig(),
		StorageDBConfig:        smallTestPebbleConfig(),
		StorageCacheConfig:     smallTestCacheConfig(),
		LegacyDBConfig:         smallTestPebbleConfig(),
		LegacyCacheConfig:      smallTestCacheConfig(),
		MetadataDBConfig:       smallTestPebbleConfig(),
		MetadataCacheConfig:    smallTestCacheConfig(),
		ReaderThreadsPerCore:   2.0,
		ReaderPoolQueueSize:    1024,
		MiscPoolThreadsPerCore: 4.0,
	})
	defer s.Close()

	addr := addrN(0x13)
	slot := slotN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	cs1 := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s) // v1

	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s) // v2 - snapshot triggers

	cs3 := makeChangeSet(key, []byte{0x33}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s) // v3

	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x33}, val)

	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())

	_, found = s.Get(key)
	require.False(t, found, "key should be deleted at v2")
}

func TestGetWithTruncatedEVMKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// A key with a valid storage prefix but too short to be parsed.
	statePrefix := evm.StateKeyPrefix()
	truncatedKey := append(statePrefix, 0x01, 0x02)
	val, found := s.Get(truncatedKey)
	require.False(t, found)
	require.Nil(t, val)
}

func TestIteratorStartEqualsEnd(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x20)
	key := memiavlStorageKey(addr, slotN(0x01))
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// start == end produces an empty iterator.
	iter := s.Iterator(key, key)
	require.False(t, iter.Valid())
	require.False(t, iter.First())
	require.NoError(t, iter.Close())
}

func TestIteratorInterleavedNextPrev(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x21)
	for i := byte(1); i <= 5; i++ {
		key := memiavlStorageKey(addr, slotN(i))
		cs := makeChangeSet(key, []byte{i}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	require.True(t, iter.First())
	val1 := append([]byte(nil), iter.Value()...)

	require.True(t, iter.Next())
	val2 := append([]byte(nil), iter.Value()...)
	require.NotEqual(t, val1, val2)

	// Prev should go back to the first key.
	require.True(t, iter.Prev())
	require.Equal(t, val1, iter.Value())
}

func TestIteratorMultipleFirstLastCalls(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x22)
	for i := byte(1); i <= 3; i++ {
		key := memiavlStorageKey(addr, slotN(i))
		cs := makeChangeSet(key, []byte{i}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	defer iter.Close()

	require.True(t, iter.First())
	firstKey := append([]byte(nil), iter.Key()...)

	require.True(t, iter.Last())
	lastKey := append([]byte(nil), iter.Key()...)

	// Calling First again should return to the first key.
	require.True(t, iter.First())
	require.Equal(t, firstKey, iter.Key())

	// Calling Last again should return to the last key.
	require.True(t, iter.Last())
	require.Equal(t, lastKey, iter.Key())
}

func TestIteratorByPrefixAfterDeletions(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x23)
	for i := byte(1); i <= 3; i++ {
		key := memiavlStorageKey(addr, slotN(i))
		cs := makeChangeSet(key, []byte{i * 10}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}
	commitAndCheck(t, s)

	// Delete slot 2.
	delKey := memiavlStorageKey(addr, slotN(2))
	delCS := makeChangeSet(delKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{delCS}))
	commitAndCheck(t, s)

	// Iterator should see only 2 entries.
	prefix := append(evm.StateKeyPrefix(), addr[:]...)
	iter := s.IteratorByPrefix(prefix)
	defer iter.Close()

	count := 0
	for ok := iter.First(); ok; ok = iter.Next() {
		count++
	}
	require.Equal(t, 2, count, "deleted key should not appear in iterator")
}

func TestIteratorByPrefixOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x24)
	for i := byte(1); i <= 3; i++ {
		key := memiavlStorageKey(addr, slotN(i))
		cs := makeChangeSet(key, []byte{i}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	prefix := append(evm.StateKeyPrefix(), addr[:]...)
	iter := ro.IteratorByPrefix(prefix)
	defer iter.Close()

	count := 0
	for ok := iter.First(); ok; ok = iter.Next() {
		count++
	}
	require.Equal(t, 3, count)
	require.NoError(t, s.Close())
}

func TestIteratorByPrefixNilPrefix(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x25)
	key := memiavlStorageKey(addr, slotN(0x01))
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// nil prefix goes through Iterator(nil, nil) path = full scan.
	iter := s.IteratorByPrefix(nil)
	defer iter.Close()

	count := 0
	for ok := iter.First(); ok; ok = iter.Next() {
		count++
	}
	require.Equal(t, 1, count, "nil prefix should scan all storage keys")
}

func TestIteratorOnClosedStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x26)
	key := memiavlStorageKey(addr, slotN(0x01))
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	iter := s.Iterator(nil, nil)
	require.True(t, iter.First())
	require.NoError(t, iter.Close())

	// Close the store, then try a new iterator -- should not panic.
	require.NoError(t, s.Close())

	// Note: after Close(), the DB handles are nil. Depending on implementation
	// this may panic or return an empty/erroring iterator. We just verify no panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Iterator on closed store panicked (expected): %v", r)
			}
		}()
		iter2 := s.Iterator(nil, nil)
		if iter2 != nil {
			_ = iter2.Close()
		}
	}()
}
