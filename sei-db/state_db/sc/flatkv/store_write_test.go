package flatkv

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
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

	legacyKey := append([]byte{0x09}, addr[:]...)

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
		// Legacy key (codeSize: 0x09 || addr)
		{
			Key:   legacyKey,
			Value: []byte{0x00, 0x03},
		},
	}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify all 4 DBs have their LocalMeta updated to version 1 (persisted)
	for name, db := range map[string]types.KeyValueDB{
		"storageDB": s.storageDB,
		"accountDB": s.accountDB,
		"codeDB":    s.codeDB,
		"legacyDB":  s.legacyDB,
	} {
		raw, err := db.Get(metaVersionKey)
		require.NoError(t, err, "%s meta version read", name)
		require.Equal(t, int64(1), int64(binary.BigEndian.Uint64(raw)), "%s persisted version", name)
	}

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

	// Verify bytecode stored directly in codeDB (raw key = addr)
	codeRaw, err := s.codeDB.Get(addr[:])
	require.NoError(t, err)
	require.Equal(t, []byte{0x60, 0x60, 0x60}, codeRaw)

	// Verify legacy data persisted in legacyDB (full key preserved)
	legacyVal, err := s.legacyDB.Get(legacyKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 0x03}, legacyVal)
}

func TestStoreWriteEmptyCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Commit version 1 with no writes
	emptyCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

	// Commit version 2 with storage write only
	addr := Address{0x99}
	slot := Slot{0x88}
	key := memiavlStorageKey(addr, slot)
	cs := makeChangeSet(key, []byte{0x77}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 2)
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
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

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
		Name:      "evm",
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
		Name:      "evm",
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

	requireAllLocalMetaAt(t, s, 2)
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
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
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
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

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
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)
}

// =============================================================================
// Fsync Config Tests
// =============================================================================

func TestStoreFsyncConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := DefaultTestConfig(t)
		store, err := NewCommitStore(t.Context(), cfg)
		require.NoError(t, err)
		_, err = store.LoadVersion(0, false)
		require.NoError(t, err)
		defer store.Close()

		// Verify defaults
		require.False(t, store.config.Fsync)
		require.Equal(t, 0, store.config.AsyncWriteBuffer)
	})

	t.Run("FsyncDisabled", func(t *testing.T) {
		cfg := DefaultTestConfig(t)
		cfg.Fsync = false
		store, err := NewCommitStore(t.Context(), cfg)
		require.NoError(t, err)
		_, err = store.LoadVersion(0, false)
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

// =============================================================================
// Auto-snapshot triggered by SnapshotInterval
// =============================================================================

func TestAutoSnapshotTriggeredByInterval(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 2
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	flatkvDir := s.flatkvDir()
	var snapshots []int64
	_ = traverseSnapshots(flatkvDir, true, func(v int64) (bool, error) {
		snapshots = append(snapshots, v)
		return false, nil
	})
	require.Contains(t, snapshots, int64(5), "auto-snapshot should fire at version 5")
}

func TestAutoSnapshotNotTriggeredBeforeInterval(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 10
	cfg.SnapshotKeepRecent = 2
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	flatkvDir := s.flatkvDir()
	var countBefore int
	_ = traverseSnapshots(flatkvDir, true, func(_ int64) (bool, error) {
		countBefore++
		return false, nil
	})

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	var countAfter int
	_ = traverseSnapshots(flatkvDir, true, func(_ int64) (bool, error) {
		countAfter++
		return false, nil
	})
	require.Equal(t, countBefore, countAfter, "no new auto-snapshot before interval")
}

func TestAutoSnapshotDisabledWhenIntervalZero(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 0
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	flatkvDir := s.flatkvDir()
	var countBefore int
	_ = traverseSnapshots(flatkvDir, true, func(_ int64) (bool, error) {
		countBefore++
		return false, nil
	})

	for i := 0; i < 10; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	var countAfter int
	_ = traverseSnapshots(flatkvDir, true, func(_ int64) (bool, error) {
		countAfter++
		return false, nil
	})
	require.Equal(t, countBefore, countAfter, "no new auto-snapshot when interval=0")
}

// =============================================================================
// Multiple ApplyChangeSets before Commit
// =============================================================================

func TestMultipleApplyChangeSetsBeforeCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	slot1 := Slot{0x01}
	slot2 := Slot{0x02}

	key1 := memiavlStorageKey(addr, slot1)
	key2 := memiavlStorageKey(addr, slot2)

	cs1 := makeChangeSet(key1, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	cs2 := makeChangeSet(key2, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)

	v1, ok := s.Get(key1)
	require.True(t, ok)
	require.Equal(t, []byte{0x11}, v1)

	v2, ok := s.Get(key2)
	require.True(t, ok)
	require.Equal(t, []byte{0x22}, v2)
}

func TestMultipleApplyAccountFieldsPreservesOther(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xBB}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	codeHash := CodeHash{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}

	cs1 := makeChangeSet(nonceKey, []byte{0, 0, 0, 0, 0, 0, 0, 42}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	cs2 := makeChangeSet(codeHashKey, codeHash[:], false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	nonceVal, ok := s.Get(nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceVal, "nonce should be preserved after codehash update")

	chVal, ok := s.Get(codeHashKey)
	require.True(t, ok)
	require.Equal(t, codeHash[:], chVal)
}

// =============================================================================
// LtHash determinism
// =============================================================================

func TestLtHashDeterministicAcrossReopen(t *testing.T) {
	writeAndGetHash := func() []byte {
		cfg := DefaultTestConfig(t)
		s, err := NewCommitStore(t.Context(), cfg)
		require.NoError(t, err)
		_, err = s.LoadVersion(0, false)
		require.NoError(t, err)

		commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0xAA})
		commitStorageEntry(t, s, Address{0x02}, Slot{0x02}, []byte{0xBB})
		commitStorageEntry(t, s, Address{0x03}, Slot{0x03}, []byte{0xCC})

		hash := s.RootHash()
		require.NoError(t, s.Close())
		return hash
	}

	h1 := writeAndGetHash()
	h2 := writeAndGetHash()
	require.Equal(t, h1, h2, "same writes must produce same LtHash")
}

func TestLtHashUpdatedByDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xDD}
	slot := Slot{0xEE}
	key := memiavlStorageKey(addr, slot)

	cs1 := makeChangeSet(key, []byte{0xFF}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)
	hashAfterWrite := s.RootHash()

	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)
	hashAfterDelete := s.RootHash()

	require.NotEqual(t, hashAfterWrite, hashAfterDelete, "delete should change LtHash")
}

func TestLtHashAccountFieldMerge(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xCC}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	codeHash := CodeHash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: nonceKey, Value: []byte{0, 0, 0, 0, 0, 0, 0, 10}},
				{Key: codeHashKey, Value: codeHash[:]},
			},
		},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify both nonce and codehash merged into one AccountValue
	raw, found, err := s.accountDB.Get(AccountKey(addr), true)
	require.NoError(t, err)
	require.True(t, found)
	av, err := DecodeAccountValue(raw)
	require.NoError(t, err)
	require.Equal(t, uint64(10), av.Nonce)
	require.Equal(t, codeHash, av.CodeHash)
}

// =============================================================================
// Overwrite same key in single block
// =============================================================================

func TestOverwriteSameKeyInSingleBlock(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xEE}
	slot := Slot{0xFF}
	key := memiavlStorageKey(addr, slot)

	pairs := []*iavl.KVPair{
		{Key: key, Value: []byte{0x01}},
		{Key: key, Value: []byte{0x02}},
	}
	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	v, ok := s.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x02}, v, "last write should win")
}

// =============================================================================
// Empty commit advances version
// =============================================================================

func TestEmptyCommitAdvancesVersion(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()

	require.NoError(t, s.ApplyChangeSets(nil))
	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), v)

	hashAfter := s.RootHash()
	require.Equal(t, hashBefore, hashAfter, "empty commit should not change LtHash")
}

// =============================================================================
// Fsync enabled
// =============================================================================

func TestStoreFsyncEnabled(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.Fsync = true
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	require.True(t, s.config.Fsync)

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	require.Equal(t, int64(1), s.Version())

	v, ok := s.Get(memiavlStorageKey(Address{0x01}, Slot{0x01}))
	require.True(t, ok)
	require.Equal(t, []byte{0x01}, v)
}

// =============================================================================
// lastSnapshotTime is set after WriteSnapshot
// =============================================================================

func TestLastSnapshotTimeUpdated(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	require.True(t, s.lastSnapshotTime.IsZero())

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	require.NoError(t, s.WriteSnapshot(""))

	require.False(t, s.lastSnapshotTime.IsZero())
	require.True(t, time.Since(s.lastSnapshotTime) < time.Second)
}

// =============================================================================
// WAL records all changesets
// =============================================================================

func TestWALRecordsChangesets(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, Address{0x02}, Slot{0x02}, []byte{0xBB})
	commitStorageEntry(t, s, Address{0x03}, Slot{0x03}, []byte{0xCC})

	first, _ := s.changelog.FirstOffset()
	last, _ := s.changelog.LastOffset()
	require.Greater(t, last, uint64(0))

	var versions []int64
	err = s.changelog.Replay(first, last, func(_ uint64, entry proto.ChangelogEntry) error {
		versions = append(versions, entry.Version)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, versions)

	require.NoError(t, s.Close())
}

// =============================================================================
// Delete Semantics — Asymmetric Account Read Behavior (W-P0-3)
// =============================================================================

func TestDeleteSemanticsCodehashAsymmetry(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xDD}
	ch := codeHashN(0x99)

	cs := namedCS(
		noncePair(addr, 42),
		codeHashPair(addr, ch),
		codePair(addr, []byte{0x60}),
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	delCS := namedCS(
		nonceDeletePair(addr),
		codeHashDeletePair(addr),
		codeDeletePair(addr),
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{delCS}))
	commitAndCheck(t, s)

	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal, found := s.Get(nonceKey)
	require.True(t, found, "nonce Get should return found=true after delete")
	require.Equal(t, make([]byte, NonceLen), nonceVal, "nonce should be zero bytes")

	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	chVal, found := s.Get(chKey)
	require.False(t, found, "codehash Get should return found=false after delete")
	require.Nil(t, chVal)

	require.False(t, s.Has(chKey), "Has(codehash) should be false after delete")
	require.True(t, s.Has(nonceKey), "Has(nonce) should be true after delete")

	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	_, found = s.Get(codeKey)
	require.False(t, found, "code should be physically deleted")

	raw, err := s.accountDB.Get(AccountKey(addr))
	require.NoError(t, err, "accountDB entry should persist after account delete")
	require.NotNil(t, raw)
}

// =============================================================================
// Cross-ApplyChangeSets Ordering (W-P0-5)
// =============================================================================

func TestCrossApplyChangeSetsOrdering(t *testing.T) {
	t.Run("write-then-delete", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := Address{0x01}
		slot := Slot{0x01}

		cs1 := namedCS(storagePair(addr, slot, []byte{0xAA}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
		_, found := s.Get(key)
		require.False(t, found, "write-then-delete: key should be gone")
	})

	t.Run("delete-then-write", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := Address{0x02}
		slot := Slot{0x02}

		cs0 := namedCS(storagePair(addr, slot, []byte{0x11}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(storagePair(addr, slot, []byte{0xBB}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
		val, found := s.Get(key)
		require.True(t, found, "delete-then-write: key should exist")
		require.Equal(t, []byte{0xBB}, val)
	})

}

// =============================================================================
// Empty Commit WAL Payload Distinction (W-P0-6)
// =============================================================================

func TestEmptyCommitWALPayloadsDiffer(t *testing.T) {
	sNil := setupTestStore(t)
	defer sNil.Close()
	require.NoError(t, sNil.ApplyChangeSets(nil))
	commitAndCheck(t, sNil)

	sEmpty := setupTestStore(t)
	defer sEmpty.Close()
	emptyCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: nil},
	}
	require.NoError(t, sEmpty.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, sEmpty)

	nilFirst, _ := sNil.changelog.FirstOffset()
	nilLast, _ := sNil.changelog.LastOffset()
	var nilEntry proto.ChangelogEntry
	err := sNil.changelog.Replay(nilFirst, nilLast, func(_ uint64, e proto.ChangelogEntry) error {
		nilEntry = e
		return nil
	})
	require.NoError(t, err)

	emptyFirst, _ := sEmpty.changelog.FirstOffset()
	emptyLast, _ := sEmpty.changelog.LastOffset()
	var emptyEntry proto.ChangelogEntry
	err = sEmpty.changelog.Replay(emptyFirst, emptyLast, func(_ uint64, e proto.ChangelogEntry) error {
		emptyEntry = e
		return nil
	})
	require.NoError(t, err)

	require.Len(t, nilEntry.Changesets, 0, "nil ApplyChangeSets produces 0 WAL changesets")
	require.Len(t, emptyEntry.Changesets, 1, "[empty NamedChangeSet] produces 1 WAL changeset")
}

// =============================================================================
// Sub-DB Entry Count (W-P0-10)
// =============================================================================

func TestSubDBEntryCount(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr1 := Address{0x01}
	addr2 := Address{0x02}
	slot1 := Slot{0x01}
	slot2 := Slot{0x02}

	cs := namedCS(
		storagePair(addr1, slot1, []byte{0xAA}),
		storagePair(addr2, slot2, []byte{0xBB}),
		noncePair(addr1, 1),
		codeHashPair(addr1, codeHashN(0x11)),
		noncePair(addr2, 2),
		codeHashPair(addr2, codeHashN(0x22)),
		codePair(addr1, []byte{0x60}),
		codePair(addr2, []byte{0x61}),
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	require.Equal(t, 2, countLiveEntries(t, s.storageDB), "storageDB should have 2 entries")
	require.Equal(t, 2, countLiveEntries(t, s.accountDB), "accountDB should have 2 entries")
	require.Equal(t, 2, countLiveEntries(t, s.codeDB), "codeDB should have 2 entries")

	cs2 := namedCS(storagePair(addr1, slot1, []byte{0xCC}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)
	require.Equal(t, 2, countLiveEntries(t, s.storageDB), "overwrite should not increase count")

	cs3 := namedCS(storageDeletePair(addr1, slot1))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)
	require.Equal(t, 1, countLiveEntries(t, s.storageDB), "delete should decrease count")

	cs4 := namedCS(nonceDeletePair(addr1))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs4}))
	commitAndCheck(t, s)
	require.Equal(t, 2, countLiveEntries(t, s.accountDB), "account delete should not decrease count")
}

// =============================================================================
// ApplyChangeSets Input Validation Error Paths
// =============================================================================

func TestApplyChangeSetsInvalidNonceLength(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x01}
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{
					Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
					Value: []byte{0x01, 0x02, 0x03}, // 3 bytes, expected 8
				},
			},
		},
	}
	err := s.ApplyChangeSets([]*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid nonce value length")
}

func TestApplyChangeSetsInvalidCodehashLength(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x01}
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{
					Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]),
					Value: []byte{0x01, 0x02}, // 2 bytes, expected 32
				},
			},
		},
	}
	err := s.ApplyChangeSets([]*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid codehash value length")
}

// =============================================================================
// Cross-ApplyChangeSets Account Field Ordering
// =============================================================================

func TestCrossApplyChangeSetsAccountOrdering(t *testing.T) {
	t.Run("nonce-write-then-delete", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := addrN(0x01)
		cs1 := namedCS(noncePair(addr, 42))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(nonceDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
		val, found := s.Get(key)
		require.True(t, found, "nonce delete zeroes but row persists")
		require.Equal(t, make([]byte, NonceLen), val)
	})

	t.Run("nonce-delete-then-write", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := addrN(0x02)
		cs0 := namedCS(noncePair(addr, 10))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(nonceDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(noncePair(addr, 99))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
		val, found := s.Get(key)
		require.True(t, found)
		require.Equal(t, uint64(99), bytesToNonce(val))
	})

	t.Run("codehash-write-then-delete", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := addrN(0x03)
		cs1 := namedCS(codeHashPair(addr, codeHashN(0xFF)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(codeHashDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
		_, found := s.Get(key)
		require.False(t, found, "codehash delete -> not found (asymmetric with nonce)")
	})

	t.Run("codehash-delete-then-write", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := addrN(0x04)
		cs0 := namedCS(codeHashPair(addr, codeHashN(0xAA)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(codeHashDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(codeHashPair(addr, codeHashN(0xBB)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
		val, found := s.Get(key)
		require.True(t, found, "codehash should be restored after delete-then-write")
		expected := codeHashN(0xBB)
		require.Equal(t, expected[:], val)
	})
}

func bytesToNonce(b []byte) uint64 {
	if len(b) != NonceLen {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// =============================================================================
// AccountValue Encoding Transition (40 → 72 → 40 bytes)
// =============================================================================

func TestAccountValueEncodingTransition(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)

	// Step 1: Write nonce only → EOA encoding (40 bytes)
	cs1 := namedCS(noncePair(addr, 7))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	raw1, err := s.accountDB.Get(AccountKey(addr))
	require.NoError(t, err)
	require.Equal(t, accountValueEOALen, len(raw1), "nonce-only should produce EOA encoding (40 bytes)")

	// Step 2: Add codehash → contract encoding (72 bytes)
	cs2 := namedCS(codeHashPair(addr, codeHashN(0xAB)))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	raw2, err := s.accountDB.Get(AccountKey(addr))
	require.NoError(t, err)
	require.Equal(t, accountValueContractLen, len(raw2), "nonce+codehash should produce contract encoding (72 bytes)")

	av2, err := DecodeAccountValue(raw2)
	require.NoError(t, err)
	require.Equal(t, uint64(7), av2.Nonce, "nonce should be preserved after codehash write")
	require.Equal(t, codeHashN(0xAB), av2.CodeHash)

	// Step 3: Delete codehash → back to EOA encoding (40 bytes)
	cs3 := namedCS(codeHashDeletePair(addr))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)

	raw3, err := s.accountDB.Get(AccountKey(addr))
	require.NoError(t, err)
	require.Equal(t, accountValueEOALen, len(raw3), "codehash delete should shrink back to EOA encoding (40 bytes)")

	av3, err := DecodeAccountValue(raw3)
	require.NoError(t, err)
	require.Equal(t, uint64(7), av3.Nonce, "nonce should survive codehash deletion")
	require.Equal(t, CodeHash{}, av3.CodeHash, "codehash should be zero after delete")
}

// =============================================================================
// Write Test Helpers
// =============================================================================

func countLiveEntries(t *testing.T, db types.KeyValueDB) int {
	t.Helper()
	iter, err := db.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		if isMetaKey(iter.Key()) {
			continue
		}
		count++
	}
	require.NoError(t, iter.Error())
	return count
}

func requireAllLocalMetaAt(t *testing.T, s *CommitStore, ver int64) {
	t.Helper()
	require.Equal(t, ver, s.localMeta[storageDBDir].CommittedVersion)
	require.Equal(t, ver, s.localMeta[accountDBDir].CommittedVersion)
	require.Equal(t, ver, s.localMeta[codeDBDir].CommittedVersion)
	require.Equal(t, ver, s.localMeta[legacyDBDir].CommittedVersion)
}
