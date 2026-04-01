package flatkv

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
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
	value := padLeft32(0x33)
	key := memiavlStorageKey(addr, slot)

	// No data initially
	_, found, err := s.Get(key)
	require.NoError(t, err)
	require.False(t, found)

	// Apply changeset (adds to pending writes)
	cs := makeChangeSet(key, value, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be readable from pending writes
	got, found, err := s.Get(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, value, got)

	// Commit
	commitAndCheck(t, s)

	// Should still be readable after commit
	got, found, err = s.Get(key)
	require.NoError(t, err)
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
	cs1 := makeChangeSet(key, padLeft32(0x66), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	_, found, err := s.Get(key)
	require.NoError(t, err)
	require.True(t, found)

	// Apply delete (pending)
	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Should not be found (pending delete)
	_, found, err = s.Get(key)
	require.NoError(t, err)
	require.False(t, found)

	// Commit delete
	commitAndCheck(t, s)

	// Still should not be found
	_, found, err = s.Get(key)
	require.NoError(t, err)
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

	var err error
	var found bool
	for _, key := range nonStorageKeys {
		_, found, err = s.Get(key)
		require.NoError(t, err)
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
	found, err := s.Has(key)
	require.NoError(t, err)
	require.False(t, found)

	// Write and commit
	cs := makeChangeSet(key, padLeft32(0xAA), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Now should exist
	found, err = s.Has(key)
	require.NoError(t, err)
	require.True(t, found)
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
	_, found, err := s.Get(legacyKey)
	require.NoError(t, err)
	require.False(t, found)

	// Apply changeset
	cs := makeChangeSet(legacyKey, []byte{0x00, 0x40}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be readable from pending writes
	got, found, err := s.Get(legacyKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x40}, got)

	// Commit and still readable
	commitAndCheck(t, s)
	got, found, err = s.Get(legacyKey)
	require.NoError(t, err)
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

	_, found, err := s.Get(legacyKey)
	require.NoError(t, err)
	require.True(t, found)

	// Apply delete (pending)
	cs2 := makeChangeSet(legacyKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Should not be found (pending delete)
	_, found, err = s.Get(legacyKey)
	require.NoError(t, err)
	require.False(t, found)

	// Commit delete
	commitAndCheck(t, s)
	_, found, err = s.Get(legacyKey)
	require.NoError(t, err)
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
	cs1 := makeChangeSet(key, padLeft32(0x77), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	got, found, err := s.Get(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, padLeft32(0x77), got)

	// Delete
	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Should not exist
	_, found, err = s.Get(key)
	require.NoError(t, err)
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
	value := padLeft32(0xCC)
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
		pairs[i] = &iavl.KVPair{Key: key, Value: padLeft32(e.value)}
	}

	cs := &proto.NamedChangeSet{
		Name: "evm",
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
		cs := makeChangeSet(key, padLeft32(i*10), false)
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
		cs := makeChangeSet(key, padLeft32(i*10), false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	}

	// Write slots for addr2
	for i := byte(1); i <= 2; i++ {
		slot := Slot{i}
		key := memiavlStorageKey(addr2, slot)
		cs := makeChangeSet(key, padLeft32(i*20), false)
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
// GetBlockHeightModified
// =============================================================================

func TestGetBlockHeightModified_Storage(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x01}
	slot := Slot{0x02}
	key := memiavlStorageKey(addr, slot)

	// Not found initially
	bh, found, err := s.GetBlockHeightModified(key)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)

	// Write at version 1
	cs := makeChangeSet(key, padLeft32(0x42), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s) // version 1

	bh, found, err = s.GetBlockHeightModified(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)

	// Overwrite at version 2
	cs2 := makeChangeSet(key, padLeft32(0x99), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s) // version 2

	bh, found, err = s.GetBlockHeightModified(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(2), bh)
}

func TestGetBlockHeightModified_Nonce(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x10}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonceVal, 7)

	// Not found initially
	bh, found, err := s.GetBlockHeightModified(nonceKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)

	// Write at version 1
	cs := makeChangeSet(nonceKey, nonceVal, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	bh, found, err = s.GetBlockHeightModified(nonceKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)
}

func TestGetBlockHeightModified_CodeHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x20}
	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	codeHashVal := vtype.CodeHash{0xAA}

	// Not found initially
	bh, found, err := s.GetBlockHeightModified(codeHashKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)

	// Write nonce + codehash together (account data is a single row)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonceVal, 1)
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: nonceKey, Value: nonceVal},
				{Key: codeHashKey, Value: codeHashVal[:]},
			},
		},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	bh, found, err = s.GetBlockHeightModified(codeHashKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)
}

func TestGetBlockHeightModified_Code(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x30}
	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	bytecode := []byte{0x60, 0x80, 0x60, 0x40}

	// Not found initially
	bh, found, err := s.GetBlockHeightModified(codeKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)

	// Write at version 1
	cs := makeChangeSet(codeKey, bytecode, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	bh, found, err = s.GetBlockHeightModified(codeKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)
}

func TestGetBlockHeightModified_Legacy(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x40}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Not found initially
	bh, found, err := s.GetBlockHeightModified(legacyKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)

	// Write at version 1
	cs := makeChangeSet(legacyKey, []byte{0xCA, 0xFE}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	bh, found, err = s.GetBlockHeightModified(legacyKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)
}

func TestGetBlockHeightModified_UnknownKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	bh, found, err := s.GetBlockHeightModified([]byte{0xFF, 0xFF})
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)
}

func TestGetBlockHeightModified_DeletedKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x50}
	slot := Slot{0x60}
	key := memiavlStorageKey(addr, slot)

	// Write then delete
	cs1 := makeChangeSet(key, padLeft32(0x01), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	bh, found, err := s.GetBlockHeightModified(key)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, int64(-1), bh)
}

func TestGetBlockHeightModified_PendingWrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x70}
	slot := Slot{0x80}
	key := memiavlStorageKey(addr, slot)

	// Apply but don't commit — data is pending
	cs := makeChangeSet(key, padLeft32(0x42), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	bh, found, err := s.GetBlockHeightModified(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)
}

func TestGetBlockHeightModified_UpdateBumpsHeight(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x90}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonce1 := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonce1, 1)
	nonce2 := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonce2, 2)

	// Write at version 1
	cs1 := makeChangeSet(nonceKey, nonce1, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	bh, found, err := s.GetBlockHeightModified(nonceKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), bh)

	// Update at version 2
	cs2 := makeChangeSet(nonceKey, nonce2, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	bh, found, err = s.GetBlockHeightModified(nonceKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(2), bh)
}
