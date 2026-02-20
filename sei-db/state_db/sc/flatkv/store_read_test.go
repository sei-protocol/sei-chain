package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/cosmos/iavl"
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

	pairs := make([]*iavl.KVPair, len(entries))
	for i, e := range entries {
		key := memiavlStorageKey(addr, e.slot)
		pairs[i] = &iavl.KVPair{Key: key, Value: []byte{e.value}}
	}

	cs := &proto.NamedChangeSet{
		Name: "test",
		Changeset: iavl.ChangeSet{
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
