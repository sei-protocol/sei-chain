package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Multi-DB Write (Account, Code, Storage)
// =============================================================================

func TestStoreNonStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x99}
	codeHash := CodeHash{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00}

	// Write non-storage keys (now supported with AccountValue)
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])

	// Write nonce (8 bytes)
	cs1 := makeChangeSet(nonceKey, []byte{0, 0, 0, 0, 0, 0, 0, 17}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	// Write codehash (32 bytes)
	cs2 := makeChangeSet(codeHashKey, codeHash[:], false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)

	// Nonce should be found
	nonceValue, found := s.Get(nonceKey)
	require.True(t, found, "nonce should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 17}, nonceValue)

	// CodeHash should be found
	codeHashValue, found := s.Get(codeHashKey)
	require.True(t, found, "codehash should be found")
	require.Equal(t, codeHash[:], codeHashValue)
}

func TestStoreWriteAllDBs(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x12, 0x34}
	slot := Slot{0x56, 0x78}

	// Create changesets for all three key types
	pairs := []*iavl.KVPair{
		// Storage key
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)),
			Value: []byte{0x11, 0x22},
		},
		// Account nonce key
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 42}, // nonce = 42
		},
		// Code key - keyed by address, not codeHash
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
			Value: []byte{0x60, 0x60, 0x60}, // some bytecode
		},
	}

	cs := &proto.NamedChangeSet{
		Name: "test",
		Changeset: iavl.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify all three DBs have their LocalMeta updated to version 1
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion, "storageDB should be at version 1")
	require.Equal(t, int64(1), s.localMeta[accountDBDir].CommittedVersion, "accountDB should be at version 1")
	require.Equal(t, int64(1), s.localMeta[codeDBDir].CommittedVersion, "codeDB should be at version 1")

	// Verify LocalMeta is persisted in each DB
	storageMetaBytes, err := s.storageDB.Get(DBLocalMetaKey)
	require.NoError(t, err)
	storageMeta, err := UnmarshalLocalMeta(storageMetaBytes)
	require.NoError(t, err)
	require.Equal(t, int64(1), storageMeta.CommittedVersion)

	accountMetaBytes, err := s.accountDB.Get(DBLocalMetaKey)
	require.NoError(t, err)
	accountMeta, err := UnmarshalLocalMeta(accountMetaBytes)
	require.NoError(t, err)
	require.Equal(t, int64(1), accountMeta.CommittedVersion)

	codeMetaBytes, err := s.codeDB.Get(DBLocalMetaKey)
	require.NoError(t, err)
	codeMeta, err := UnmarshalLocalMeta(codeMetaBytes)
	require.NoError(t, err)
	require.Equal(t, int64(1), codeMeta.CommittedVersion)

	// Verify storage data was written
	storageData, err := s.storageDB.Get(StorageKey(addr, slot))
	require.NoError(t, err)
	require.Equal(t, []byte{0x11, 0x22}, storageData)

	// Verify account and code data was written
	// Use Store.Get method which handles the kind prefix correctly
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceValue, found := s.Get(nonceKey)
	require.True(t, found, "Nonce should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceValue)

	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	codeValue, found := s.Get(codeKey)
	require.True(t, found, "Code should be found")
	require.Equal(t, []byte{0x60, 0x60, 0x60}, codeValue)
}

func TestStoreWriteEmptyCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Commit version 1 with no writes
	emptyCS := &proto.NamedChangeSet{
		Name:      "empty",
		Changeset: iavl.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	// All DBs should have LocalMeta at version 1
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[codeDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[legacyDBDir].CommittedVersion)

	// Commit version 2 with storage write only
	addr := Address{0x99}
	slot := Slot{0x88}
	key := memiavlStorageKey(addr, slot)
	cs := makeChangeSet(key, []byte{0x77}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// All DBs should have LocalMeta at version 2, even though only storage had data
	require.Equal(t, int64(2), s.localMeta[storageDBDir].CommittedVersion)
	require.Equal(t, int64(2), s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, int64(2), s.localMeta[codeDBDir].CommittedVersion)
	require.Equal(t, int64(2), s.localMeta[legacyDBDir].CommittedVersion)
}

func TestStoreWriteAccountAndCode(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr1 := Address{0xAA}
	addr2 := Address{0xBB}

	// Write account nonces and codes
	// Note: Code is keyed by address (not codeHash) per x/evm/types/keys.go
	pairs := []*iavl.KVPair{
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr1[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 1}, // nonce = 1
		},
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr2[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 2}, // nonce = 2
		},
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr1[:]),
			Value: []byte{0x60, 0x80},
		},
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr2[:]),
			Value: []byte{0x60, 0xA0},
		},
	}

	cs := &proto.NamedChangeSet{
		Name: "test",
		Changeset: iavl.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify LocalMeta is updated in all DBs for version consistency
	require.Equal(t, int64(1), s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[codeDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)

	// Verify account data was written
	nonceKey1 := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr1[:])
	nonce1, found := s.Get(nonceKey1)
	require.True(t, found, "Nonce1 should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 1}, nonce1)

	nonceKey2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr2[:])
	nonce2, found := s.Get(nonceKey2)
	require.True(t, found, "Nonce2 should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 2}, nonce2)

	// Verify code data was written
	codeKey1 := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr1[:])
	code1, found := s.Get(codeKey1)
	require.True(t, found, "Code1 should be found")
	require.Equal(t, []byte{0x60, 0x80}, code1)

	codeKey2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr2[:])
	code2, found := s.Get(codeKey2)
	require.True(t, found, "Code2 should be found")
	require.Equal(t, []byte{0x60, 0xA0}, code2)

	// Verify LtHash was updated (includes all keys)
	hash := s.RootHash()
	require.NotNil(t, hash)
	require.Equal(t, 32, len(hash))
}

func TestStoreWriteDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xCC}
	slot := Slot{0xDD}

	// Write initial data
	// Note: Code is keyed by address per x/evm/types/keys.go
	pairs := []*iavl.KVPair{
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)),
			Value: []byte{0x11},
		},
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 1},
		},
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
			Value: []byte{0x60},
		},
	}

	cs1 := &proto.NamedChangeSet{
		Name:      "write",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Delete storage and code (actual deletes)
	// For account, "delete" means setting fields to zero in AccountValue
	deletePairs := []*iavl.KVPair{
		{
			Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)),
			Delete: true,
		},
		{
			Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
			Delete: true, // Sets nonce to 0 in AccountValue
		},
		{
			Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
			Delete: true,
		},
	}

	cs2 := &proto.NamedChangeSet{
		Name:      "delete",
		Changeset: iavl.ChangeSet{Pairs: deletePairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Verify storage is deleted
	_, err := s.storageDB.Get(StorageKey(addr, slot))
	require.Error(t, err, "storage should be deleted")

	// Verify nonce is set to 0 (delete in AccountValue context)
	nonceKeyDel := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceValue, found := s.Get(nonceKeyDel)
	require.True(t, found, "nonce entry should still exist but be zero")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 0}, nonceValue, "nonce should be 0 after delete")

	// Verify code is deleted
	codeKeyDel := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	_, found = s.Get(codeKeyDel)
	require.False(t, found, "code should be deleted")

	// LocalMeta should still be at version 2
	require.Equal(t, int64(2), s.localMeta[storageDBDir].CommittedVersion)
	require.Equal(t, int64(2), s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, int64(2), s.localMeta[codeDBDir].CommittedVersion)
}

func TestAccountValueStorage(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xFF, 0xFF}
	expectedCodeHash := CodeHash{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB}

	// Write both Nonce and CodeHash for the same address
	// AccountValue stores: balance(32) || nonce(8) || codehash(32)
	pairs := []*iavl.KVPair{
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 42}, // nonce = 42
		},
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]),
			Value: expectedCodeHash[:], // 32-byte codehash
		},
	}

	cs := &proto.NamedChangeSet{
		Name:      "test",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// AccountValue structure: one entry per address containing both nonce and codehash
	require.Equal(t, 1, len(s.accountWrites), "should have 1 account write (AccountValue)")

	// Commit
	commitAndCheck(t, s)

	// Verify AccountValue is stored in accountDB with addr as key
	stored, err := s.accountDB.Get(addr[:])
	require.NoError(t, err)
	require.NotNil(t, stored)

	// Decode and verify
	av, err := DecodeAccountValue(stored)
	require.NoError(t, err)
	require.Equal(t, uint64(42), av.Nonce, "Nonce should be 42")
	require.Equal(t, expectedCodeHash, av.CodeHash, "CodeHash should match")
	require.Equal(t, Balance{}, av.Balance, "Balance should be zero")

	// Get method should return individual fields
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceValue, found := s.Get(nonceKey)
	require.True(t, found, "Nonce should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceValue, "Nonce should be 42")

	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	codeHashValue, found := s.Get(codeHashKey)
	require.True(t, found, "CodeHash should be found")
	require.Equal(t, expectedCodeHash[:], codeHashValue, "CodeHash should match")
}

// =============================================================================
// Legacy DB Write Tests
// =============================================================================

func TestStoreWriteLegacyKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}

	// CodeSize key (0x09 || addr) goes to legacy
	codeSizeKey := append([]byte{0x09}, addr[:]...)
	codeSizeValue := []byte{0x00, 0x10}

	cs := makeChangeSet(codeSizeKey, codeSizeValue, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should be in legacyWrites pending buffer
	require.Len(t, s.legacyWrites, 1)

	commitAndCheck(t, s)

	// Verify legacyDB LocalMeta is updated
	require.Equal(t, int64(1), s.localMeta[legacyDBDir].CommittedVersion)

	// Verify data persisted in legacyDB (full key preserved)
	stored, err := s.legacyDB.Get(codeSizeKey)
	require.NoError(t, err)
	require.Equal(t, codeSizeValue, stored)
}

func TestStoreWriteLegacyAndOptimizedKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x12, 0x34}
	slot := Slot{0x56, 0x78}

	pairs := []*iavl.KVPair{
		// Storage (optimized)
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)),
			Value: []byte{0x11, 0x22},
		},
		// Nonce (optimized)
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 42},
		},
		// Code (optimized)
		{
			Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
			Value: []byte{0x60, 0x60, 0x60},
		},
		// CodeSize → legacy (0x09 || addr)
		{
			Key:   append([]byte{0x09}, addr[:]...),
			Value: []byte{0x00, 0x03},
		},
	}

	cs := &proto.NamedChangeSet{
		Name:      "test",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// All four DBs should have LocalMeta at version 1
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[codeDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[legacyDBDir].CommittedVersion)

	// Verify legacy data persisted
	codeSizeKey := append([]byte{0x09}, addr[:]...)
	stored, err := s.legacyDB.Get(codeSizeKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 0x03}, stored)
}

func TestStoreWriteDeleteLegacyKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xCC}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Write
	cs1 := makeChangeSet(legacyKey, []byte{0x00, 0x10}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	got, found := s.Get(legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x10}, got)

	// Delete
	cs2 := makeChangeSet(legacyKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Should not be found
	_, found = s.Get(legacyKey)
	require.False(t, found)
}

func TestStoreLegacyKeyIncludedInLtHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Get initial hash
	hash1 := s.RootHash()

	// Write a legacy key
	addr := Address{0xDD}
	legacyKey := append([]byte{0x09}, addr[:]...)
	cs := makeChangeSet(legacyKey, []byte{0x00, 0x20}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// LtHash should change after applying legacy key changeset
	hash2 := s.RootHash()
	require.NotEqual(t, hash1, hash2, "LtHash should change when legacy key is written")

	commitAndCheck(t, s)

	// After commit, hash should be stable
	hash3 := s.RootHash()
	require.Equal(t, hash2, hash3)
}

func TestStoreLegacyEmptyCommitLocalMeta(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Commit with no writes — all DBs including legacy should advance LocalMeta
	emptyCS := &proto.NamedChangeSet{
		Name:      "empty",
		Changeset: iavl.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[codeDBDir].CommittedVersion)
	require.Equal(t, int64(1), s.localMeta[legacyDBDir].CommittedVersion)
}

// =============================================================================
// Fsync Config Tests
// =============================================================================

func TestStoreFsyncConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		dir := t.TempDir()
		store := NewCommitStore(dir, nil, DefaultConfig())
		_, err := store.LoadVersion(0)
		require.NoError(t, err)
		defer store.Close()

		// Verify defaults
		require.False(t, store.config.Fsync)
		require.Equal(t, 0, store.config.AsyncWriteBuffer)
	})

	t.Run("FsyncDisabled", func(t *testing.T) {
		dir := t.TempDir()
		store := NewCommitStore(dir, nil, Config{
			Fsync: false,
		})
		_, err := store.LoadVersion(0)
		require.NoError(t, err)
		defer store.Close()

		addr := Address{0xAA}
		slot := Slot{0xBB}
		key := memiavlStorageKey(addr, slot)

		// Write and commit with fsync disabled
		cs := makeChangeSet(key, []byte{0xCC}, false)
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, store)

		// Data should be readable
		got, found := store.Get(key)
		require.True(t, found)
		require.Equal(t, []byte{0xCC}, got)

		// Version should be updated
		require.Equal(t, int64(1), store.Version())
	})
}
