package flatkv

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// =============================================================================
// Multi-DB Write (Account, Code, Storage)
// =============================================================================

func TestStoreNonStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x99}
	codeHash := vtype.CodeHash{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00}

	// Write non-storage keys (now supported with AccountValue)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	// Write nonce (8 bytes)
	cs1 := makeChangeSet(nonceKey, []byte{0, 0, 0, 0, 0, 0, 0, 17}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	// Write codehash (32 bytes)
	cs2 := makeChangeSet(codeHashKey, codeHash[:], false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)

	// Nonce should be found
	nonceValue, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 17}, nonceValue)

	// CodeHash should be found
	codeHashValue, found := s.Get(keys.EVMStoreKey, codeHashKey)
	require.True(t, found, "codehash should be found")
	require.Equal(t, codeHash[:], codeHashValue)
}

func TestStoreWriteAllDBs(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x12, 0x34}
	slot := ktype.Slot{0x56, 0x78}

	legacyKey := append([]byte{0x09}, addr[:]...)

	pairs := []*proto.KVPair{
		// Storage key
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
			Value: padLeft32(0x11, 0x22),
		},
		// Account nonce key
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 42}, // nonce = 42
		},
		// Code key - keyed by address, not codeHash
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
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
		Changeset: proto.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify all 4 DBs have their LocalMeta updated to version 1 (persisted)
	for _, ndb := range s.namedDataDBs() {
		raw, err := ndb.db.Get(ktype.MetaVersionKey)
		require.NoError(t, err, "%s meta version read", ndb.dir)
		require.Equal(t, int64(1), int64(binary.BigEndian.Uint64(raw)), "%s persisted version", ndb.dir)
	}

	// Verify storage data was written (via Store.Get which deserializes)
	storageMemiavlKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	storageValue, found := s.Get(keys.EVMStoreKey, storageMemiavlKey)
	require.True(t, found, "Storage should be found")
	require.Equal(t, padLeft32(0x11, 0x22), storageValue)

	// Verify account and code data was written
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceValue, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "Nonce should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceValue)

	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	codeValue, found := s.Get(keys.EVMStoreKey, codeKey)
	require.True(t, found, "Code should be found")
	require.Equal(t, []byte{0x60, 0x60, 0x60}, codeValue)

	// Verify legacy data persisted (via Store.Get which deserializes)
	legacyVal, found := s.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found, "Legacy should be found")
	require.Equal(t, []byte{0x00, 0x03}, legacyVal)
}

func TestStoreWriteEmptyCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Commit version 1 with no writes
	emptyCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

	// Commit version 2 with storage write only
	addr := ktype.Address{0x99}
	slot := ktype.Slot{0x88}
	key := evmStorageKey(addr, slot)
	cs := makeChangeSet(key, padLeft32(0x77), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 2)
}

func TestStoreWriteAccountAndCode(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr1 := ktype.Address{0xAA}
	addr2 := ktype.Address{0xBB}

	// Write account nonces and codes
	// Note: Code is keyed by address (not codeHash) per x/evm/types/keys.go
	pairs := []*proto.KVPair{
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr1[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 1}, // nonce = 1
		},
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr2[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 2}, // nonce = 2
		},
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr1[:]),
			Value: []byte{0x60, 0x80},
		},
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr2[:]),
			Value: []byte{0x60, 0xA0},
		},
	}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

	// Verify account data was written
	nonceKey1 := keys.BuildEVMKey(keys.EVMKeyNonce, addr1[:])
	nonce1, found := s.Get(keys.EVMStoreKey, nonceKey1)
	require.True(t, found, "Nonce1 should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 1}, nonce1)

	nonceKey2 := keys.BuildEVMKey(keys.EVMKeyNonce, addr2[:])
	nonce2, found := s.Get(keys.EVMStoreKey, nonceKey2)
	require.True(t, found, "Nonce2 should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 2}, nonce2)

	// Verify code data was written
	codeKey1 := keys.BuildEVMKey(keys.EVMKeyCode, addr1[:])
	code1, found := s.Get(keys.EVMStoreKey, codeKey1)
	require.True(t, found, "Code1 should be found")
	require.Equal(t, []byte{0x60, 0x80}, code1)

	codeKey2 := keys.BuildEVMKey(keys.EVMKeyCode, addr2[:])
	code2, found := s.Get(keys.EVMStoreKey, codeKey2)
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

	addr := ktype.Address{0xCC}
	slot := ktype.Slot{0xDD}

	// Write initial data
	// Note: Code is keyed by address per x/evm/types/keys.go
	pairs := []*proto.KVPair{
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
			Value: padLeft32(0x11),
		},
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 1},
		},
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
			Value: []byte{0x60},
		},
	}

	cs1 := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Delete storage and code (actual deletes)
	// For account, "delete" means setting fields to zero in AccountValue
	deletePairs := []*proto.KVPair{
		{
			Key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
			Delete: true,
		},
		{
			Key:    keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
			Delete: true, // Sets nonce to 0 in AccountValue
		},
		{
			Key:    keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
			Delete: true,
		},
	}

	cs2 := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: deletePairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Verify storage is deleted
	_, err := s.storageDB.Get(storagePhysKey(addr, slot))
	require.Error(t, err, "storage should be deleted")

	// Nonce was the only account field written (no codehash). After delete,
	// all fields are zero so the accountDB row is physically deleted.
	nonceKeyDel := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceValue, found := s.Get(keys.EVMStoreKey, nonceKeyDel)
	require.False(t, found, "nonce should not be found after account row deletion")
	require.Nil(t, nonceValue)

	// Verify code is deleted
	codeKeyDel := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	_, found = s.Get(keys.EVMStoreKey, codeKeyDel)
	require.False(t, found, "code should be deleted")

	requireAllLocalMetaAt(t, s, 2)
}

func TestAccountValueStorage(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xFF, 0xFF}
	expectedCodeHash := vtype.CodeHash{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB}

	// Write both Nonce and CodeHash for the same address
	// AccountValue stores: balance(32) || nonce(8) || codehash(32)
	pairs := []*proto.KVPair{
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 42}, // nonce = 42
		},
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
			Value: expectedCodeHash[:], // 32-byte codehash
		},
	}

	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// AccountValue structure: one entry per address containing both nonce and codehash
	require.Equal(t, 1, len(s.accountWrites), "should have 1 account write (AccountValue)")

	// Commit
	commitAndCheck(t, s)

	// Verify AccountValue is stored in accountDB with physical key
	stored, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err)
	require.NotNil(t, stored)

	// Decode and verify
	ad, err := vtype.DeserializeAccountData(stored)
	require.NoError(t, err)
	require.Equal(t, uint64(42), ad.GetNonce(), "Nonce should be 42")
	require.Equal(t, &expectedCodeHash, ad.GetCodeHash(), "CodeHash should match")
	var zeroBalance vtype.Balance
	require.Equal(t, &zeroBalance, ad.GetBalance(), "Balance should be zero")

	// Get method should return individual fields
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceValue, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "Nonce should be found")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceValue, "Nonce should be 42")

	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	codeHashValue, found := s.Get(keys.EVMStoreKey, codeHashKey)
	require.True(t, found, "CodeHash should be found")
	require.Equal(t, expectedCodeHash[:], codeHashValue, "CodeHash should match")
}

// =============================================================================
// Legacy DB Write Tests
// =============================================================================

func TestStoreWriteLegacyKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xAA}

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

	// Verify data persisted (via Store.Get which deserializes)
	got, found := s.Get(keys.EVMStoreKey, codeSizeKey)
	require.True(t, found)
	require.Equal(t, codeSizeValue, got)
}

func TestStoreWriteLegacyAndOptimizedKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x12, 0x34}
	slot := ktype.Slot{0x56, 0x78}

	pairs := []*proto.KVPair{
		// Storage (optimized)
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
			Value: padLeft32(0x11, 0x22),
		},
		// Nonce (optimized)
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
			Value: []byte{0, 0, 0, 0, 0, 0, 0, 42},
		},
		// Code (optimized)
		{
			Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
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
		Changeset: proto.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

	// Verify legacy data persisted (via Store.Get which deserializes)
	codeSizeKey := append([]byte{0x09}, addr[:]...)
	got, found := s.Get(keys.EVMStoreKey, codeSizeKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x03}, got)
}

func TestStoreWriteDeleteLegacyKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xCC}
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Write
	cs1 := makeChangeSet(legacyKey, []byte{0x00, 0x10}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	got, found := s.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x10}, got)

	// Delete
	cs2 := makeChangeSet(legacyKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Should not be found
	_, found = s.Get(keys.EVMStoreKey, legacyKey)
	require.False(t, found)
}

func TestStoreLegacyKeyIncludedInLtHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Get initial hash
	hash1 := s.RootHash()

	// Write a legacy key
	addr := ktype.Address{0xDD}
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
		Changeset: proto.ChangeSet{Pairs: nil},
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
		cfg := config.DefaultTestConfig(t)
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
		cfg := config.DefaultTestConfig(t)
		cfg.Fsync = false
		store, err := NewCommitStore(t.Context(), cfg)
		require.NoError(t, err)
		_, err = store.LoadVersion(0, false)
		require.NoError(t, err)
		defer store.Close()

		addr := ktype.Address{0xAA}
		slot := ktype.Slot{0xBB}
		key := evmStorageKey(addr, slot)

		// Write and commit with fsync disabled
		cs := makeChangeSet(key, padLeft32(0xCC), false)
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, store)

		// Data should be readable
		got, found := store.Get(keys.EVMStoreKey, key)
		require.True(t, found)
		require.Equal(t, padLeft32(0xCC), got)

		// Version should be updated
		require.Equal(t, int64(1), store.Version())
	})
}

// =============================================================================
// Auto-snapshot triggered by SnapshotInterval
// =============================================================================

func TestAutoSnapshotTriggeredByInterval(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 2
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
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
	cfg := config.DefaultTestConfig(t)
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
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	var countAfter int
	_ = traverseSnapshots(flatkvDir, true, func(_ int64) (bool, error) {
		countAfter++
		return false, nil
	})
	require.Equal(t, countBefore, countAfter, "no new auto-snapshot before interval")
}

func TestAutoSnapshotDisabledWhenIntervalZero(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
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
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
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

	addr := ktype.Address{0xAA}
	slot1 := ktype.Slot{0x01}
	slot2 := ktype.Slot{0x02}

	key1 := evmStorageKey(addr, slot1)
	key2 := evmStorageKey(addr, slot2)

	cs1 := makeChangeSet(key1, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	cs2 := makeChangeSet(key2, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)

	v1, ok := s.Get(keys.EVMStoreKey, key1)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x11), v1)

	v2, ok := s.Get(keys.EVMStoreKey, key2)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x22), v2)
}

func TestMultipleApplyAccountFieldsPreservesOther(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xBB}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	codeHash := vtype.CodeHash{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}

	cs1 := makeChangeSet(nonceKey, []byte{0, 0, 0, 0, 0, 0, 0, 42}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	cs2 := makeChangeSet(codeHashKey, codeHash[:], false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	nonceVal, ok := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceVal, "nonce should be preserved after codehash update")

	chVal, ok := s.Get(keys.EVMStoreKey, codeHashKey)
	require.True(t, ok)
	require.Equal(t, codeHash[:], chVal)
}

// =============================================================================
// LtHash determinism
// =============================================================================

func TestLtHashDeterministicAcrossReopen(t *testing.T) {
	writeAndGetHash := func() []byte {
		cfg := config.DefaultTestConfig(t)
		s, err := NewCommitStore(t.Context(), cfg)
		require.NoError(t, err)
		_, err = s.LoadVersion(0, false)
		require.NoError(t, err)

		commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
		commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
		commitStorageEntry(t, s, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})

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

	addr := ktype.Address{0xDD}
	slot := ktype.Slot{0xEE}
	key := evmStorageKey(addr, slot)

	cs1 := makeChangeSet(key, padLeft32(0xFF), false)
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

	addr := ktype.Address{0xCC}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	codeHash := vtype.CodeHash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: nonceKey, Value: []byte{0, 0, 0, 0, 0, 0, 0, 10}},
				{Key: codeHashKey, Value: codeHash[:]},
			},
		},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	require.Len(t, s.accountWrites, 1, "both nonce and codehash should merge into one AccountValue")

	accountWrite := s.accountWrites[string(accountPhysKey(addr))]
	require.NotNil(t, accountWrite)
	require.Equal(t, uint64(10), accountWrite.GetNonce())
	require.Equal(t, &codeHash, accountWrite.GetCodeHash())
}

// =============================================================================
// Overwrite same key in single block
// =============================================================================

func TestOverwriteSameKeyInSingleBlock(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xEE}
	slot := ktype.Slot{0xFF}
	key := evmStorageKey(addr, slot)

	pairs := []*proto.KVPair{
		{Key: key, Value: padLeft32(0x01)},
		{Key: key, Value: padLeft32(0x02)},
	}
	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	v, ok := s.Get(keys.EVMStoreKey, key)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x02), v, "last write should win")
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
	cfg := config.DefaultTestConfig(t)
	cfg.Fsync = true
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	require.True(t, s.config.Fsync)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0x01})
	require.Equal(t, int64(1), s.Version())

	v, ok := s.Get(keys.EVMStoreKey, evmStorageKey(ktype.Address{0x01}, ktype.Slot{0x01}))
	require.True(t, ok)
	require.Equal(t, padLeft32(0x01), v)
}

// =============================================================================
// lastSnapshotTime is set after WriteSnapshot
// =============================================================================

func TestLastSnapshotTimeUpdated(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	require.True(t, s.lastSnapshotTime.IsZero())

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0x01})
	require.NoError(t, s.WriteSnapshot(""))

	require.False(t, s.lastSnapshotTime.IsZero())
	require.True(t, time.Since(s.lastSnapshotTime) < time.Second)
}

// =============================================================================
// WAL records all changesets
// =============================================================================

func TestWALRecordsChangesets(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	commitStorageEntry(t, s, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})

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

	addr := ktype.Address{0xDD}
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

	// After deleting all account fields, the row is physically deleted (Account Row GC).
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceVal, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should not be found after all-zero account row deletion")
	require.Nil(t, nonceVal)

	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	chVal, found := s.Get(keys.EVMStoreKey, chKey)
	require.False(t, found, "codehash should not be found after row deletion")
	require.Nil(t, chVal)

	hasCodeHash := s.Has(keys.EVMStoreKey, chKey)
	require.False(t, hasCodeHash, "Has(codehash) should be false after delete")
	hasNonce := s.Has(keys.EVMStoreKey, nonceKey)
	require.False(t, hasNonce, "Has(nonce) should be false after row deletion")

	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	_, found = s.Get(keys.EVMStoreKey, codeKey)
	require.False(t, found, "code should be physically deleted")

	_, err := s.accountDB.Get(accountPhysKey(addr))
	require.Error(t, err, "accountDB row should be physically deleted when all fields are zero")
}

// =============================================================================
// Cross-ApplyChangeSets Ordering (W-P0-5)
// =============================================================================

func TestCrossApplyChangeSetsOrdering(t *testing.T) {
	t.Run("write-then-delete", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := ktype.Address{0x01}
		slot := ktype.Slot{0x01}

		cs1 := namedCS(storagePair(addr, slot, []byte{0xAA}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
		_, found := s.Get(keys.EVMStoreKey, key)
		require.False(t, found, "write-then-delete: key should be gone")
	})

	t.Run("delete-then-write", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := ktype.Address{0x02}
		slot := ktype.Slot{0x02}

		cs0 := namedCS(storagePair(addr, slot, []byte{0x11}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(storagePair(addr, slot, []byte{0xBB}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)

		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
		val, found := s.Get(keys.EVMStoreKey, key)
		require.True(t, found, "delete-then-write: key should exist")
		require.Equal(t, padLeft32(0xBB), val)
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
		Changeset: proto.ChangeSet{Pairs: nil},
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

	addr1 := ktype.Address{0x01}
	addr2 := ktype.Address{0x02}
	slot1 := ktype.Slot{0x01}
	slot2 := ktype.Slot{0x02}

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

	addr := ktype.Address{0x01}
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{
					Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
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

	addr := ktype.Address{0x01}
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{
					Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
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

		// With Account Row GC, nonce-only account becomes all-zero → row deleted
		key := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
		_, found := s.Get(keys.EVMStoreKey, key)
		require.False(t, found, "nonce-only account should be deleted after nonce delete")
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

		key := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
		val, found := s.Get(keys.EVMStoreKey, key)
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

		key := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
		_, found := s.Get(keys.EVMStoreKey, key)
		require.False(t, found, "codehash-only account: delete → all-zero → row deleted")
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

		key := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
		val, found := s.Get(keys.EVMStoreKey, key)
		require.True(t, found, "codehash should be restored after delete-then-write")
		expected := codeHashN(0xBB)
		require.Equal(t, expected[:], val)
	})
}

func bytesToNonce(b []byte) uint64 {
	if len(b) != vtype.NonceLen {
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

	// Step 1: Write nonce only (AccountData always 81 bytes)
	cs1 := namedCS(noncePair(addr, 7))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	raw1, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err)
	ad1, err := vtype.DeserializeAccountData(raw1)
	require.NoError(t, err)
	require.Equal(t, uint64(7), ad1.GetNonce())
	var zeroHash vtype.CodeHash
	require.Equal(t, &zeroHash, ad1.GetCodeHash(), "nonce-only should have zero codehash")

	// Step 2: Add codehash
	cs2 := namedCS(codeHashPair(addr, codeHashN(0xAB)))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	raw2, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err)
	ad2, err := vtype.DeserializeAccountData(raw2)
	require.NoError(t, err)
	require.Equal(t, uint64(7), ad2.GetNonce(), "nonce should be preserved after codehash write")
	expectedCH := codeHashN(0xAB)
	require.Equal(t, &expectedCH, ad2.GetCodeHash())

	// Step 3: Delete codehash → back to zero codehash
	cs3 := namedCS(codeHashDeletePair(addr))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)

	raw3, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err)
	ad3, err := vtype.DeserializeAccountData(raw3)
	require.NoError(t, err)
	require.Equal(t, uint64(7), ad3.GetNonce(), "nonce should survive codehash deletion")
	require.Equal(t, &zeroHash, ad3.GetCodeHash(), "codehash should be zero after delete")
}

// =============================================================================
// Account Row GC
// =============================================================================

func TestAccountRowDeletedWhenAllFieldsZero(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA1)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	ch := codeHashN(0xBB)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 42), codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr), codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	_, err := s.accountDB.Get(accountPhysKey(addr))
	require.Error(t, err, "accountDB row should be physically deleted")

	nonceVal, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should not be found after row deletion")
	require.Nil(t, nonceVal)

	chVal, found := s.Get(keys.EVMStoreKey, chKey)
	require.False(t, found, "codehash should not be found after row deletion")
	require.Nil(t, chVal)
}

func TestAccountRowPersistsWhenPartiallyZero(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA2)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	ch := codeHashN(0xCC)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 7), codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	raw, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err, "accountDB row should still exist after partial delete")
	require.NotNil(t, raw)

	nonceVal, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce should still be readable")
	require.Equal(t, nonceBytes(7), nonceVal)
}

func TestAccountRowDeleteThenRecreate(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA3)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 10)),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	_, err := s.accountDB.Get(accountPhysKey(addr))
	require.Error(t, err, "row should be deleted after all-zero")

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 99)),
	}))
	commitAndCheck(t, s)

	raw, err := s.accountDB.Get(accountPhysKey(addr))
	require.NoError(t, err, "row should be recreated")
	require.NotNil(t, raw)

	nonceVal, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found)
	require.Equal(t, nonceBytes(99), nonceVal)
}

// =============================================================================
// Write-Zero Triggers GC (EIP-161 alignment)
// =============================================================================

// TestAccountRowGCOnWriteZero verifies that writing a zero value (as opposed
// to a Delete) still triggers row GC when the result is an all-zero account.
// This is critical for future balance support where SetBalance(0) is a write,
// not a delete.
func TestAccountRowGCOnWriteZero(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xA4)

	// Block 1: write nonce = 5
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 5)),
	}))
	commitAndCheck(t, s)

	// Block 2: write nonce = 0 (write, not delete)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 0)),
	}))
	commitAndCheck(t, s)

	_, err := s.accountDB.Get(accountPhysKey(addr))
	require.Error(t, err, "accountDB row should be GC'd when write-zero makes account empty")

	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	_, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should not be found after write-zero GC")
}

// TestAccountRowGCWriteZeroOrderIndependent verifies that the order of
// delete + write-zero operations within a single changeset does not affect
// whether GC occurs.
func TestAccountRowGCWriteZeroOrderIndependent(t *testing.T) {
	for _, name := range []string{"delete-then-write-zero", "write-zero-then-delete"} {
		t.Run(name, func(t *testing.T) {
			s := setupTestStore(t)
			defer s.Close()

			addr := addrN(0xA5)
			ch := codeHashN(0xDD)

			// Block 1: nonce=5 + codehash
			require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
				namedCS(noncePair(addr, 5), codeHashPair(addr, ch)),
			}))
			commitAndCheck(t, s)

			// Block 2: one field deleted, one field written to zero
			var pairs []*proto.KVPair
			if name == "delete-then-write-zero" {
				pairs = []*proto.KVPair{codeHashDeletePair(addr), noncePair(addr, 0)}
			} else {
				pairs = []*proto.KVPair{noncePair(addr, 0), codeHashDeletePair(addr)}
			}
			require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
			commitAndCheck(t, s)

			_, err := s.accountDB.Get(accountPhysKey(addr))
			require.Error(t, err, "accountDB row should be GC'd regardless of operation order")
		})
	}
}

// =============================================================================
// Write Test Helpers
// =============================================================================

// TestLtHashExistingAccountNonceUpdate is a focused regression test for the
// oldAccountRawValues bug: when an account already exists in the DB and a new
// block updates its nonce (the most common case — every tx increments sender
// nonce), the LtHash delta must MixOut the old encoded AccountValue before
// MixIn'ing the new one. The bug sets oldAccountRawValues[addr] = nil instead
// of the DB value when s.accountWrites has no pending entry, causing the
// MixOut to be skipped and the LtHash to diverge from ground truth.
func TestLtHashExistingAccountNonceUpdate(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xE1)

	// Block 1: create account with nonce=1 (new account — oldAccountRawValues
	// correctly nil here since nothing exists in DB).
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1) // should pass: new account, nil old is correct

	// Block 2: update nonce to 2. The account now EXISTS in accountDB with
	// encoded(nonce=1). The buggy code sets oldAccountRawValues[addr] = nil
	// because s.accountWrites is empty after the block-1 commit cleared it.
	// The correct old value is the DB's encoded(nonce=1).
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 2)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2) // FAILS: incremental skipped MixOut of old value
}

func countLiveEntries(t *testing.T, db types.KeyValueDB) int {
	t.Helper()
	iter, err := db.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	defer iter.Close()

	count := 0
	for ; iter.Valid(); iter.Next() {
		if ktype.IsMetaKey(iter.Key()) {
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

func TestApplyChangeSetsNilInput(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()
	require.NoError(t, s.ApplyChangeSets(nil))
	require.Equal(t, hashBefore, s.RootHash(), "nil input should not change hash")
}

func TestApplyChangeSetsEmptySlice(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{}))
	require.Equal(t, hashBefore, s.RootHash(), "empty slice should not change hash")
}

func TestApplyChangeSetsNonEVMModuleRoutesToLegacy(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()

	cs := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("some-bank-key"), Value: []byte("some-value")},
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	require.NotEqual(t, hashBefore, s.RootHash(), "legacy-routed key changes hash")
	require.Len(t, s.legacyWrites, 1)
	require.Len(t, s.storageWrites, 0)
	require.Len(t, s.pendingChangeSets, 1)

	// Physical key in legacyWrites should be module-prefixed: "bank/some-bank-key"
	physKey := string(ktype.ModulePhysicalKey("bank", []byte("some-bank-key")))
	_, found := s.legacyWrites[physKey]
	require.True(t, found, "legacyWrites should contain module-prefixed key %q", physKey)

	// Persist and verify round-trip via raw legacyDB lookup
	commitAndCheck(t, s)
	raw, err := s.legacyDB.Get([]byte(physKey))
	require.NoError(t, err)
	require.NotNil(t, raw, "legacyDB should persist module-prefixed key")
}

func TestApplyChangeSetsMixedEVMAndNonEVM(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xAA)
	slot := slotN(0x01)
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	evmCS := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: padLeft32(0x42)},
		}},
	}
	bankCS := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("bank-key"), Value: []byte("bank-value")},
		}},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{evmCS, bankCS}))

	// EVM storage write should exist.
	require.Len(t, s.storageWrites, 1)

	// The EVM value should be readable via pending writes.
	val, found := s.Get(keys.EVMStoreKey, storageKey)
	require.True(t, found)
	require.Equal(t, padLeft32(0x42), val)

	// Bank key should be in legacyWrites with module prefix.
	bankPhysKey := string(ktype.ModulePhysicalKey("bank", []byte("bank-key")))
	_, found = s.legacyWrites[bankPhysKey]
	require.True(t, found, "bank key should be in legacyWrites with module prefix")
	require.Len(t, s.legacyWrites, 1)
}

func TestApplyChangeSetsEmptyPairsVsNilPairs(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// nil Pairs: entire named CS skipped (not appended to pendingChangeSets processing).
	nilPairsCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: nil},
	}

	// empty Pairs: iterates zero times, still referenced.
	emptyPairsCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{}},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{nilPairsCS, emptyPairsCS}))
	require.Len(t, s.storageWrites, 0)
	require.Len(t, s.accountWrites, 0)
}

func TestApplyChangeSetsOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	err = ro.ApplyChangeSets([]*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestApplyChangeSetsInvalidAddressLength(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// A well-formed nonce key: prefix(1) + addr(20) = 21 bytes.
	// Build one manually with correct prefix but wrong addr length.
	// ParseEVMKey checks len(key) != len(noncePrefix)+20 and falls back to legacy.
	// To actually trigger "invalid address length" in ApplyChangeSets, we need
	// ParseEVMKey to return EVMKeyNonce with wrong-length keyBytes.
	// This only happens for the correct total length. So instead, test via
	// a key that ParseEVMKey routes to EVMKeyNonce (21 bytes total),
	// but the len(keyBytes) != ktype.AddressLen check in getAccountData fails.
	//
	// Actually, ParseEVMKey always strips the prefix correctly for 21-byte keys.
	// The address will always be 20 bytes. So this error path is unreachable
	// through normal key construction. Instead, verify that malformed nonce keys
	// (wrong total length) are routed to legacy.
	truncatedNonceKey := append([]byte{0x0a}, make([]byte, 15)...) // 16 bytes total
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: truncatedNonceKey, Value: nonceBytes(1)},
		}},
	}
	// Routed to EVMKeyLegacy (not Nonce), so no address validation error.
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	require.Len(t, s.legacyWrites, 1, "malformed nonce key should be treated as legacy")
	require.Len(t, s.accountWrites, 0, "should not reach account path")
}

func TestApplyChangeSetsErrorRecoveryPartialState(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xBB)
	slot := slotN(0x01)
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	// First pair: valid storage write
	// Second pair: invalid nonce length (triggers error)
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: padLeft32(0xAA)},
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0x01, 0x02}}, // wrong length
		}},
	}

	err := s.ApplyChangeSets([]*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid nonce value length")

	// The storage write may have been buffered before the error.
	// Verify the store doesn't panic and can still accept new operations.
	validCS := makeChangeSet(storageKey, padLeft32(0xBB), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{validCS}))
}

func TestApplyChangeSetsEVMKeyEmptySkipped(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{}, Value: []byte{0xAA}},
		}},
	}
	require.Error(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
}

func TestApplyChangeSetsNonPrefixedKeyGoesToLegacy(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()

	// A key with an unrecognized prefix goes to EVMKeyLegacy, not skipped.
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{0xFF, 0x01, 0x02}, Value: []byte{0xAA}},
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	require.NotEqual(t, hashBefore, s.RootHash(), "legacy key changes hash")
	require.Len(t, s.legacyWrites, 1)
}

func TestCommitWithoutPriorApply(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()

	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
	require.Equal(t, hashBefore, s.RootHash(), "hash should be unchanged after empty commit")
}

func TestDoubleCommitNoApplyBetween(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	v1, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), v1)
	hashAfterV1 := s.RootHash()

	// Second commit with no new apply.
	v2, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), v2)
	require.Equal(t, hashAfterV1, s.RootHash(), "hash unchanged between commits without apply")
}

func TestCommitOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	_, err = ro.Commit()
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestCommitVersionMonotonicAfterMultipleEmptyCommits(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	for i := int64(1); i <= 5; i++ {
		v, err := s.Commit()
		require.NoError(t, err)
		require.Equal(t, i, v)
	}
	require.Equal(t, int64(5), s.Version())
}

func TestNonEVMModuleKeyRoundTrip(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	err := s.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("balance_alice"), Value: []byte("100")},
				{Key: []byte("balance_bob"), Value: []byte("200")},
			}},
		},
		{
			Name: "_migration",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("boundary"), Value: []byte("42")},
			}},
		},
	})
	require.NoError(t, err)
	_, err = s.Commit()
	require.NoError(t, err)

	got, found := s.Get("bank", []byte("balance_alice"))
	require.True(t, found, "bank/balance_alice should be found")
	require.Equal(t, []byte("100"), got)

	got, found = s.Get("bank", []byte("balance_bob"))
	require.True(t, found, "bank/balance_bob should be found")
	require.Equal(t, []byte("200"), got)

	got, found = s.Get("_migration", []byte("boundary"))
	require.True(t, found, "_migration/boundary should be found")
	require.Equal(t, []byte("42"), got)

	require.True(t, s.Has("bank", []byte("balance_alice")))
	require.False(t, s.Has("bank", []byte("nonexistent")))
	require.False(t, s.Has("staking", []byte("balance_alice")),
		"different module should not see bank's keys")

	_, _, err = s.GetBlockHeightModified("bank", []byte("balance_alice"))
	require.Error(t, err, "non-EVM module should not support GetBlockHeightModified")
}
