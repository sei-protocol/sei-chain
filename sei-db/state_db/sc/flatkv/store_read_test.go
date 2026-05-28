package flatkv

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// =============================================================================
// Get, Has, and Pending Writes
// =============================================================================

func TestStoreGetPendingWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x11}
	slot := ktype.Slot{0x22}
	value := padLeft32(0x33)
	key := evmStorageKey(addr, slot)

	// No data initially
	_, found := s.Get(keys.EVMStoreKey, key)
	require.False(t, found)

	// Apply changeset (adds to pending writes)
	cs := makeChangeSet(key, value, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be readable from pending writes
	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, value, got)

	// Commit
	commitAndCheck(t, s)

	// Should still be readable after commit
	got, found = s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, value, got)
}

func TestStoreGetPendingDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x44}
	slot := ktype.Slot{0x55}
	key := evmStorageKey(addr, slot)

	// Write and commit
	cs1 := makeChangeSet(key, padLeft32(0x66), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	_, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)

	// Apply delete (pending)
	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Should not be found (pending delete)
	_, found = s.Get(keys.EVMStoreKey, key)
	require.False(t, found)

	// Commit delete
	commitAndCheck(t, s)

	// Still should not be found
	_, found = s.Get(keys.EVMStoreKey, key)
	require.False(t, found)
}

func TestStoreGetNonStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x77}

	// Non-storage keys should return not found (before write)
	nonStorageKeys := [][]byte{
		keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
		keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
	}

	var found bool
	for _, key := range nonStorageKeys {
		_, found = s.Get(keys.EVMStoreKey, key)
		require.False(t, found, "non-storage keys should not be found before write")
	}
}

func TestStoreHas(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x88}
	slot := ktype.Slot{0x99}
	key := evmStorageKey(addr, slot)

	// Initially not found
	found := s.Has(keys.EVMStoreKey, key)
	require.False(t, found)

	// Write and commit
	cs := makeChangeSet(key, padLeft32(0xAA), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Now should exist
	found = s.Has(keys.EVMStoreKey, key)
	require.True(t, found)
}

// =============================================================================
// Legacy Key Get Tests
// =============================================================================

func TestStoreGetLegacyPendingWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xEE}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Not found initially
	_, found := s.Get(keys.EVMStoreKey, legacyKey)
	require.False(t, found)

	// Apply changeset
	cs := makeChangeSet(legacyKey, []byte{0x00, 0x40}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be readable from pending writes
	got, found := s.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x40}, got)

	// Commit and still readable
	commitAndCheck(t, s)
	got, found = s.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x40}, got)
}

func TestStoreGetLegacyPendingDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xFF}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Write and commit
	cs1 := makeChangeSet(legacyKey, []byte{0x00, 0x80}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	_, found := s.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found)

	// Apply delete (pending)
	cs2 := makeChangeSet(legacyKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Should not be found (pending delete)
	_, found = s.Get(keys.EVMStoreKey, legacyKey)
	require.False(t, found)

	// Commit delete
	commitAndCheck(t, s)
	_, found = s.Get(keys.EVMStoreKey, legacyKey)
	require.False(t, found)
}

// =============================================================================
// Delete
// =============================================================================

func TestStoreDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x55}
	slot := ktype.Slot{0x66}
	key := evmStorageKey(addr, slot)

	// Write
	cs1 := makeChangeSet(key, padLeft32(0x77), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x77), got)

	// Delete
	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Should not exist
	_, found = s.Get(keys.EVMStoreKey, key)
	require.False(t, found)
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
	got, found := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
	require.True(t, found, "storage should be found")
	require.Equal(t, padLeft32(0x42), got)

	// Nonce
	got, found = s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found, "nonce should be found")
	require.Equal(t, uint64(7), binary.BigEndian.Uint64(got))

	// CodeHash
	got, found = s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
	require.True(t, found, "codehash should be found")
	require.Equal(t, ch[:], got)

	// Code
	got, found = s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
	require.True(t, found, "code should be found")
	require.Equal(t, bytecode, got)

	// Legacy
	got, found = s.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found, "legacy should be found")
	require.Equal(t, legacyVal, got)

	// Has should match
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, legacyKey)
	require.True(t, found)
}

func TestGetNonceFromCommittedEOA(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA2)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 42)),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce should be found for EOA")
	require.Equal(t, uint64(42), binary.BigEndian.Uint64(got))

	_, found = s.Get(keys.EVMStoreKey, chKey)
	require.False(t, found, "codehash should NOT be found for EOA")

	found = s.Has(keys.EVMStoreKey, nonceKey)
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, chKey)
	require.False(t, found)
}

func TestGetCodeHashFromCommittedContract(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA3)
	ch := codeHashN(0xCC)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1), codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(keys.EVMStoreKey, chKey)
	require.True(t, found, "codehash should be found for contract")
	require.Equal(t, ch[:], got)

	got, found = s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found)
	require.Equal(t, uint64(1), binary.BigEndian.Uint64(got))

	found = s.Has(keys.EVMStoreKey, chKey)
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, nonceKey)
	require.True(t, found)
}

func TestGetCodeFromCommittedDB(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA4)
	bytecode := []byte{0x60, 0x80, 0x52}
	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])

	// Pending code write is visible before commit
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codePair(addr, bytecode)),
	}))
	got, found := s.Get(keys.EVMStoreKey, codeKey)
	require.True(t, found, "pending code write should be visible")
	require.Equal(t, bytecode, got)

	commitAndCheck(t, s)

	// Still visible after commit
	got, found = s.Get(keys.EVMStoreKey, codeKey)
	require.True(t, found)
	require.Equal(t, bytecode, got)

	// Pending code delete hides it before commit
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeDeletePair(addr)),
	}))
	_, found = s.Get(keys.EVMStoreKey, codeKey)
	require.False(t, found, "pending code delete should hide the entry")

	commitAndCheck(t, s)
	_, found = s.Get(keys.EVMStoreKey, codeKey)
	require.False(t, found, "code should be gone after commit")
}

func TestGetUnknownKeyTypes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Nil and empty keys map to EVMKeyEmpty, which returns (nil, false)
	// without panicking.
	for _, tc := range []struct {
		name string
		key  []byte
	}{
		{"nil key", nil},
		{"empty key", []byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			val, found := s.Get(keys.EVMStoreKey, tc.key)
			require.False(t, found)
			require.Nil(t, val)
			found = s.Has(keys.EVMStoreKey, tc.key)
			require.False(t, found)
		})
	}

	// Non-empty keys that don't match a known prefix are classified as
	// EVMKeyLegacy, which is a supported type — Get/Has should not panic.
	for _, tc := range []struct {
		name string
		key  []byte
	}{
		{"single byte", []byte{0xFF}},
		{"random bytes", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"short nonce-like (2 bytes)", []byte{0x04, 0x01}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			val, found := s.Get(keys.EVMStoreKey, tc.key)
			require.False(t, found)
			require.Nil(t, val)
			found = s.Has(keys.EVMStoreKey, tc.key)
			require.False(t, found)
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
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 10), codeHashPair(addr, codeHashN(0xDD))),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr), codeHashDeletePair(addr)),
	}))

	_, nonceFound := s.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, nonceFound, "nonce should not be found after full delete (isDelete=true)")

	_, chFound := s.Get(keys.EVMStoreKey, chKey)
	require.False(t, chFound, "codehash should not be found after full delete (isDelete=true)")

	found := s.Has(keys.EVMStoreKey, nonceKey)
	require.False(t, found)
	found = s.Has(keys.EVMStoreKey, chKey)
	require.False(t, found)
}

func TestGetAccountAfterFullDeleteCommitted(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xB2)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

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
	_, nonceFound := s.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, nonceFound, "nonce should not be found after full delete + commit")

	_, chFound := s.Get(keys.EVMStoreKey, chKey)
	require.False(t, chFound, "codehash should not be found after full delete + commit")

	found := s.Has(keys.EVMStoreKey, nonceKey)
	require.False(t, found)
	found = s.Has(keys.EVMStoreKey, chKey)
	require.False(t, found)
}

func TestGetAccountAfterPartialDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xB3)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 99), codeHashPair(addr, codeHashN(0xFF))),
	}))
	commitAndCheck(t, s)

	// Delete only codehash — nonce should survive
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce should survive partial delete")
	require.Equal(t, uint64(99), binary.BigEndian.Uint64(got))

	_, found = s.Get(keys.EVMStoreKey, chKey)
	require.False(t, found, "codehash should be gone after delete")

	// Account row should still exist (EOA encoding)
	raw, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err)
	expectedEOALen := vtype.VersionLength + vtype.BlockHeightLength + vtype.BalanceLength + vtype.NonceLength
	require.Equal(t, expectedEOALen, len(raw))
}

// =============================================================================
// R-9 ~ R-11: Multi-Block Read Correctness
// =============================================================================

func TestGetAfterOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xC1)
	slot := slotN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x11})),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x11), got)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x22, 0x33})),
	}))
	commitAndCheck(t, s)

	got, found = s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x22, 0x33), got, "should return v2 value after overwrite")
}

func TestGetAfterDeleteAndRecreate(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xC2)
	slot := slotN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

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

	_, found := s.Get(keys.EVMStoreKey, key)
	require.False(t, found, "should not be found after delete")

	// v3: re-create with different value
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0xBB, 0xCC})),
	}))
	commitAndCheck(t, s)

	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0xBB, 0xCC), got, "should return v3 value after re-create")
}

func TestGetAfterReopenAllKeyTypes(t *testing.T) {
	dir := t.TempDir()

	addr := addrN(0xC3)
	slot := slotN(0x01)
	ch := codeHashN(0xAA)
	bytecode := []byte{0x60, 0x80}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Phase 1: write everything and close
	cfg := config.DefaultTestConfig(t)
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
	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dir
	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	got, found := s2.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
	require.True(t, found, "storage should survive reopen")
	require.Equal(t, padLeft32(0x42), got)

	got, found = s2.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found, "nonce should survive reopen")
	require.Equal(t, uint64(100), binary.BigEndian.Uint64(got))

	got, found = s2.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
	require.True(t, found, "codehash should survive reopen")
	require.Equal(t, ch[:], got)

	got, found = s2.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
	require.True(t, found, "code should survive reopen")
	require.Equal(t, bytecode, got)

	got, found = s2.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found, "legacy should survive reopen")
	require.Equal(t, []byte{0x77}, got)
}

// =============================================================================
// RawGlobalIterator
// =============================================================================

func TestRawGlobalIterator_LexOrderAcrossDBs(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x42)
	slot := slotN(0x01)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slot, padLeft32(0x01)),
			&proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: []byte{0x60}},
			noncePair(addr, 1),
		),
	}))
	commitAndCheck(t, s)

	storageKey := storagePhysKey(addr, slot)
	codeKey := ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:])
	accountKey := accountPhysKey(addr)

	keys := collectIterKeys(t, requireRawGlobalIterator(t, s))

	storageIdx, codeIdx, accountIdx := -1, -1, -1
	for i, key := range keys {
		switch {
		case bytes.Equal(key, storageKey):
			storageIdx = i
		case bytes.Equal(key, codeKey):
			codeIdx = i
		case bytes.Equal(key, accountKey):
			accountIdx = i
		}
	}
	require.NotEqual(t, -1, storageIdx)
	require.NotEqual(t, -1, codeIdx)
	require.NotEqual(t, -1, accountIdx)
	require.Less(t, storageIdx, codeIdx, "storage prefix 0x03 sorts before code 0x07")
	require.Less(t, codeIdx, accountIdx, "code prefix 0x07 sorts before account 0x0a")
}

func TestRawGlobalIterator_SkipsMetaKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addrN(0x01), slotN(0x02), padLeft32(0x03))),
	}))
	commitAndCheck(t, s)

	iter := requireRawGlobalIterator(t, s)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		require.False(t, ktype.IsMetaKey(iter.Key()), "iterator must skip _meta/* keys: %x", iter.Key())
	}
	require.NoError(t, iter.Error())
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
	iter := requireRawGlobalIterator(t, s)
	require.False(t, iter.Valid(), "iterator should not see pending writes")
	require.NoError(t, iter.Close())

	commitAndCheck(t, s)

	// After commit: iterator should see it
	iter = requireRawGlobalIterator(t, s)
	defer iter.Close()
	require.True(t, iter.Valid(), "iterator should see committed entry")
	require.Equal(t, storagePhysKey(addr, slot), iter.Key())
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
	count := iterCount(t, requireRawGlobalIterator(t, s))
	require.Equal(t, 3, count, "pending delete should not affect iterator")

	commitAndCheck(t, s)

	// After commit: only 2 remain
	count = iterCount(t, requireRawGlobalIterator(t, s))
	require.Equal(t, 2, count, "committed delete should remove entry from iterator")
}

// =============================================================================
// Helpers
// =============================================================================

func requireRawGlobalIterator(t *testing.T, s *CommitStore) dbm.Iterator {
	t.Helper()
	iter, err := s.RawGlobalIterator()
	require.NoError(t, err)
	require.NotNil(t, iter)
	return iter
}

func iterCount(t *testing.T, iter dbm.Iterator) int {
	t.Helper()
	defer iter.Close()
	count := 0
	for ; iter.Valid(); iter.Next() {
		count++
	}
	require.NoError(t, iter.Error())
	return count
}

func collectIterKeys(t *testing.T, iter dbm.Iterator) [][]byte {
	t.Helper()
	defer iter.Close()
	var keys [][]byte
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, bytes.Clone(iter.Key()))
	}
	require.NoError(t, iter.Error())
	return keys
}

func TestGetNilKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	val, found := s.Get(keys.EVMStoreKey, nil)
	require.False(t, found)
	require.Nil(t, val)
}

func TestGetEmptyKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	val, found := s.Get(keys.EVMStoreKey, []byte{})
	require.False(t, found)
	require.Nil(t, val)
}

func TestHasNilKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	found := s.Has(keys.EVMStoreKey, nil)
	require.False(t, found)
}

func TestHasEmptyKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	found := s.Has(keys.EVMStoreKey, []byte{})
	require.False(t, found)
}

func TestHasForAllKeyTypes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x10)
	slot := slotN(0x01)
	ch := codeHashN(0xAB)

	pairs := []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Value: padLeft32(0x11)},
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

	found := s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
	require.True(t, found)
	found = s.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
	require.True(t, found)
}

func TestHasOnPendingDeletes(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x11)
	slot := slotN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	cs := makeChangeSet(key, padLeft32(0xAA), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)
	found := s.Has(keys.EVMStoreKey, key)
	require.True(t, found)

	delCS := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{delCS}))
	found = s.Has(keys.EVMStoreKey, key)
	require.False(t, found, "Has should return false for pending-deleted key")
}

func TestHasOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x12)
	slot := slotN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	cs := makeChangeSet(key, padLeft32(0xBB), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	found := ro.Has(keys.EVMStoreKey, key)
	require.True(t, found)
	found = ro.Has(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0xFF), slotN(0xFF))))
	require.False(t, found)
	require.NoError(t, s.Close())
}

func TestGetAfterRollback(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 2
	cfg.SnapshotKeepRecent = 5
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x13)
	slot := slotN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	cs1 := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s) // v1

	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s) // v2 - snapshot triggers

	cs3 := makeChangeSet(key, padLeft32(0x33), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s) // v3

	val, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x33), val)

	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())

	_, found = s.Get(keys.EVMStoreKey, key)
	require.False(t, found, "key should be deleted at v2")
}

func TestGetWithTruncatedEVMKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// A key with a valid storage prefix but too short to be parsed.
	statePrefix := keys.StateKeyPrefix()
	truncatedKey := append(statePrefix, 0x01, 0x02)
	val, found := s.Get(keys.EVMStoreKey, truncatedKey)
	require.False(t, found)
	require.Nil(t, val)
}
