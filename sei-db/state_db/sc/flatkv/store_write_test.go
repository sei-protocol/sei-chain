package flatkv

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

	// Write codehash (32 bytes)
	cs2 := makeChangeSet(codeHashKey, codeHash[:], false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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

	miscKey := append([]byte{0x09}, addr[:]...)

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
		// Misc key (codeSize: 0x09 || addr)
		{
			Key:   miscKey,
			Value: []byte{0x00, 0x03},
		},
	}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify all 4 DBs have their LocalMeta updated to version 1 (persisted)
	for _, dir := range dataDBDirs {
		db := s.rawDBFor(dir)
		raw, err := db.Get(ktype.MetaVersionKey)
		require.NoError(t, err, "%s meta version read", dir)
		require.Equal(t, int64(1), int64(binary.BigEndian.Uint64(raw)), "%s persisted version", dir)
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

	// Verify misc data persisted (via Store.Get which deserializes)
	miscVal, found := s.Get(keys.EVMStoreKey, miscKey)
	require.True(t, found, "Misc should be found")
	require.Equal(t, []byte{0x00, 0x03}, miscVal)
}

func TestStoreWriteEmptyCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Commit version 1 with no writes
	emptyCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

	// Commit version 2 with storage write only
	addr := ktype.Address{0x99}
	slot := ktype.Slot{0x88}
	key := evmStorageKey(addr, slot)
	cs := makeChangeSet(key, padLeft32(0x77), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 2)
}

// TestCommitRejectsNonContiguousVersion verifies a forward jump in commit height is rejected by the WAL's
// contiguity rule rather than silently accepted. The version guard only rejects versions at or below
// committedVersion, so a gapped height reaches the WAL and must fail there.
//
// This is a behavior change from the old changelog, which permitted forward jumps for gapped/batch commits.
// Note the asymmetry: the *first* block written to an empty WAL may be any number, which is why a fresh
// store can start mid-chain (see TestCatchupRecoversGappedCommitBlockAfterMetadataLag) while an established
// one cannot skip.
func TestCommitRejectsNonContiguousVersion(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	key := evmStorageKey(ktype.Address{0x11}, ktype.Slot{0x22})
	require.NoError(t, s.ApplyChangeSets(1, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(0xAA), false)}))
	_, err := s.Commit(1)
	require.NoError(t, err)

	// Skipping version 2 is rejected at apply: blocks are contiguous, so the only legal height is 2.
	err = s.ApplyChangeSets(3, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(0xBB), false)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "plus one")

	// And directly at commit, for an empty block that never buffered writes.
	_, err = s.Commit(3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "committing bad version")
	require.Equal(t, int64(1), s.committedVersion, "a rejected commit must not advance the version")
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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
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
	hash := rootHash(s)
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Verify storage is deleted
	_, err := s.rawDBFor(storageDBDir).Get(storagePhysKey(addr, slot))
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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	// AccountValue structure: one row per address containing both nonce and codehash. There is no
	// staged-row count to assert on any more, so assert the row itself is there.
	requireStaged(t, s.accountStore, accountPhysKey(addr), "expected one staged AccountValue row")

	// Commit
	commitAndCheck(t, s)

	// Verify AccountValue is stored in accountDB with physical key
	stored, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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
// Misc DB Write Tests
// =============================================================================

func TestStoreWriteMiscKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xAA}

	// CodeSize key (0x09 || addr) goes to misc
	codeSizeKey := append([]byte{0x09}, addr[:]...)
	codeSizeValue := []byte{0x00, 0x10}

	cs := makeChangeSet(codeSizeKey, codeSizeValue, false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	// Should be staged in the misc store
	requireStaged(t, s.miscStore, ktype.ModulePhysicalKey(keys.EVMStoreKey, codeSizeKey))

	commitAndCheck(t, s)

	// Verify miscDB LocalMeta is updated
	require.Equal(t, int64(1), s.localMeta[miscDBDir].CommittedVersion)

	// Verify data persisted (via Store.Get which deserializes)
	got, found := s.Get(keys.EVMStoreKey, codeSizeKey)
	require.True(t, found)
	require.Equal(t, codeSizeValue, got)
}

func TestStoreWriteMiscAndOptimizedKeys(t *testing.T) {
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
		// CodeSize → misc (0x09 || addr)
		{
			Key:   append([]byte{0x09}, addr[:]...),
			Value: []byte{0x00, 0x03},
		},
	}

	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)

	// Verify misc data persisted (via Store.Get which deserializes)
	codeSizeKey := append([]byte{0x09}, addr[:]...)
	got, found := s.Get(keys.EVMStoreKey, codeSizeKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x03}, got)
}

func TestStoreWriteDeleteMiscKey(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xCC}
	miscKey := append([]byte{0x09}, addr[:]...)

	// Write
	cs1 := makeChangeSet(miscKey, []byte{0x00, 0x10}, false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	// Verify exists
	got, found := s.Get(keys.EVMStoreKey, miscKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x10}, got)

	// Delete
	cs2 := makeChangeSet(miscKey, nil, true)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// Should not be found
	_, found = s.Get(keys.EVMStoreKey, miscKey)
	require.False(t, found)
}

func TestStoreMiscKeyIncludedInLtHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Get initial hash
	hash1 := rootHash(s)

	// Write a misc key
	addr := ktype.Address{0xDD}
	miscKey := append([]byte{0x09}, addr[:]...)
	cs := makeChangeSet(miscKey, []byte{0x00, 0x20}, false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	commitAndCheck(t, s)

	// LtHash should change once the misc key's block is committed
	hash2 := rootHash(s)
	require.NotEqual(t, hash1, hash2, "LtHash should change when misc key is written")

	// Reading it again reports the same thing
	hash3 := rootHash(s)
	require.Equal(t, hash2, hash3)
}

func TestStoreMiscEmptyCommitLocalMeta(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Commit with no writes — all DBs including misc should advance LocalMeta
	emptyCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: nil},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	requireAllLocalMetaAt(t, s, 1)
}

// =============================================================================
// Fsync Config Tests
// =============================================================================

func TestStoreFsyncConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := config.DefaultTestConfig(t)
		store, err := newCommitStoreWithWAL(t.Context(), cfg)
		require.NoError(t, err)
		err = store.LoadLatest()
		require.NoError(t, err)
		defer store.Close()

		// Verify defaults
		require.False(t, store.config.Fsync)
		require.Equal(t, 0, store.config.AsyncWriteBuffer)
	})

	t.Run("FsyncDisabled", func(t *testing.T) {
		cfg := config.DefaultTestConfig(t)
		cfg.Fsync = false
		store, err := newCommitStoreWithWAL(t.Context(), cfg)
		require.NoError(t, err)
		err = store.LoadLatest()
		require.NoError(t, err)
		defer store.Close()

		addr := ktype.Address{0xAA}
		slot := ktype.Slot{0xBB}
		key := evmStorageKey(addr, slot)

		// Write and commit with fsync disabled
		cs := makeChangeSet(key, padLeft32(0xCC), false)
		require.NoError(t, store.ApplyChangeSets(store.Version()+1, []*proto.NamedChangeSet{cs}))
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

// A failed periodic snapshot must fail a commit rather than being logged and discarded. The writer
// latches its first failure and reports it from every later call, so a checkpoint that failed with no
// caller to return to still stops the node. Swallowing it would report blocks as committed whose data
// will never reach disk, and the caller, which is required to halt on the first error, would never
// learn it had one.
//
// The failure is forced with directory permissions: the snapshot cannot create its temporary directory
// under the flatkv root. The WAL and the databases live in subdirectories that already exist, so they
// are unaffected.
func TestCommitFailsWhenPeriodicSnapshotFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("forces the failure with directory permissions, which root ignores")
	}

	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 2
	s := setupTestStoreWithConfig(t, cfg)
	defer func() { _ = s.Close() }()

	// Block 1 does not trip the interval, so it must succeed.
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xaa})

	dir := s.flatkvDir()
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, info.Mode().Perm()) })

	// Block 2 trips it.
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x02}, ktype.Slot{0x02}))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: make([]byte, 32)}}},
	}}))

	// The commit that trips the interval only hands the block to the writer, so it succeeds.
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	// Waiting for the writer surfaces the failure it latched.
	err = s.FlushSnapshots()
	require.Error(t, err, "a failed snapshot must be reported, not swallowed")
	require.ErrorContains(t, err, "create snapshot tmp dir",
		"the error must name what actually failed")

	// And the node halts: every later commit reports the same failure.
	key = keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x03}, ktype.Slot{0x03}))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: make([]byte, 32)}}},
	}}))
	_, err = s.Commit(s.Version() + 1)
	require.Error(t, err, "a bricked snapshot writer must fail every later commit")
	require.ErrorContains(t, err, "auto snapshot",
		"the error must name the snapshot as the cause rather than being swallowed")
}

// The store's contract makes every error fatal, so a hand-back failure during teardown has to reach the
// caller of Close rather than only the log.
func TestCloseReportsReleaseFailure(t *testing.T) {
	s := setupTestStore(t)

	// Retire the genuine holder first, then install one over failing stubs, so the real stores are not
	// left holding anything when they are torn down below.
	require.NoError(t, s.lastSealed.Close())
	sealed, _ := bricksOnRelease(t, s.Version())
	installed, err := newAtomicStoreView(sealed)
	require.NoError(t, err)
	s.lastSealed = installed

	err = s.Close()
	require.Error(t, err)
	require.ErrorContains(t, err, "release sealed views")
}

func TestAutoSnapshotTriggeredByInterval(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 2
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

	cs2 := makeChangeSet(key2, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	cs2 := makeChangeSet(codeHashKey, codeHash[:], false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	nonceVal, ok := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 42}, nonceVal, "nonce should be preserved after codehash update")

	chVal, ok := s.Get(keys.EVMStoreKey, codeHashKey)
	require.True(t, ok)
	require.Equal(t, codeHash[:], chVal)
}

// A balance write carries only the balance, so it has to be merged onto the account as it already
// stands. This is the case that breaks first if the balance kind is left out of the set of kinds whose
// accounts are read back before the merge: the write lands on an empty account and takes the nonce and
// the code hash with it.
func TestBalanceWritePreservesOtherAccountFields(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xBB}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	balanceKey := keys.BuildEVMKey(keys.EVMKeyBalance, addr[:])
	codeHash := codeHashN(0x7C)
	balance := balanceN(42)

	cs1 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: nonceKey, Value: nonceBytes(9)},
				{Key: codeHashKey, Value: codeHash[:]},
			},
		},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1,
		[]*proto.NamedChangeSet{makeChangeSet(balanceKey, balance[:], false)}))
	commitAndCheck(t, s)

	nonceVal, ok := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, ok)
	require.Equal(t, nonceBytes(9), nonceVal, "nonce should be preserved after balance update")

	chVal, ok := s.Get(keys.EVMStoreKey, codeHashKey)
	require.True(t, ok)
	require.Equal(t, codeHash[:], chVal, "code hash should be preserved after balance update")

	balVal, ok := s.Get(keys.EVMStoreKey, balanceKey)
	require.True(t, ok)
	require.Equal(t, balance[:], balVal)
}

// All three account fields written in one block land in one physical row.
func TestAccountFieldsMergeIntoOneRow(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xCD}
	codeHash := codeHashN(0x11)
	balance := balanceN(7)

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: nonceBytes(3)},
				{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: codeHash[:]},
				{Key: keys.BuildEVMKey(keys.EVMKeyBalance, addr[:]), Value: balance[:]},
			},
		},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	accountWrite := stagedRow(t, s.accountStore, accountPhysKey(addr), vtype.DeserializeAccountData)
	require.NotNil(t, accountWrite)
	require.Equal(t, uint64(3), accountWrite.GetNonce())
	require.Equal(t, &codeHash, accountWrite.GetCodeHash())
	require.Equal(t, &balance, accountWrite.GetBalance())
}

// A balance is the only field an account needs to exist, and zeroing it is how one is deleted, so the
// row goes away with it.
func TestBalanceOnlyAccountDeletedWhenZeroed(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xCE}
	balanceKey := keys.BuildEVMKey(keys.EVMKeyBalance, addr[:])
	balance := balanceN(5)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1,
		[]*proto.NamedChangeSet{makeChangeSet(balanceKey, balance[:], false)}))
	commitAndCheck(t, s)

	count, err := CountKeys(s)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1,
		[]*proto.NamedChangeSet{makeChangeSet(balanceKey, nil, true)}))
	commitAndCheck(t, s)

	_, ok := s.Get(keys.EVMStoreKey, balanceKey)
	require.False(t, ok)

	count, err = CountKeys(s)
	require.NoError(t, err)
	require.Zero(t, count, "zeroing the last field must remove the account row")
}

// =============================================================================
// LtHash determinism
// =============================================================================

func TestLtHashDeterministicAcrossReopen(t *testing.T) {
	writeAndGetHash := func() []byte {
		cfg := config.DefaultTestConfig(t)
		s, err := newCommitStoreWithWAL(t.Context(), cfg)
		require.NoError(t, err)
		err = s.LoadLatest()
		require.NoError(t, err)

		commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
		commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
		commitStorageEntry(t, s, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})

		hash := rootHash(s)
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)
	hashAfterWrite := rootHash(s)

	cs2 := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)
	hashAfterDelete := rootHash(s)

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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	// Both changeset entries merge into one AccountValue: the single staged row carries the nonce and
	// the codehash together.
	accountWrite := stagedRow(t, s.accountStore, accountPhysKey(addr), vtype.DeserializeAccountData)
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
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

	hashBefore := rootHash(s)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, nil))
	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v)

	hashAfter := rootHash(s)
	require.Equal(t, hashBefore, hashAfter, "empty commit should not change LtHash")
}

// =============================================================================
// Fsync enabled
// =============================================================================

func TestStoreFsyncEnabled(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.Fsync = true
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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
// WAL records all changesets
// =============================================================================

func TestWALRecordsChangesets(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	commitStorageEntry(t, s, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})

	require.Equal(t, []uint64{1, 2, 3}, walBlockNumbers(t, s))

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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	delCS := namedCS(
		nonceDeletePair(addr),
		codeHashDeletePair(addr),
		codeDeletePair(addr),
	)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{delCS}))
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

	_, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(storagePair(addr, slot, []byte{0xBB}))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
	require.NoError(t, sNil.ApplyChangeSets(sNil.Version()+1, nil))
	commitAndCheck(t, sNil)

	sEmpty := setupTestStore(t)
	defer sEmpty.Close()
	emptyCS := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: nil},
	}
	require.NoError(t, sEmpty.ApplyChangeSets(sEmpty.Version()+1, []*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, sEmpty)

	nilChangesets := singleWALBlockChangesets(t, sNil)
	emptyChangesets := singleWALBlockChangesets(t, sEmpty)

	require.Len(t, nilChangesets, 0, "nil ApplyChangeSets produces 0 WAL changesets")
	require.Len(t, emptyChangesets, 1, "[empty NamedChangeSet] produces 1 WAL changeset")
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	require.Equal(t, 2, countLiveEntries(t, s.rawDBFor(storageDBDir)), "storageDB should have 2 entries")
	require.Equal(t, 2, countLiveEntries(t, s.rawDBFor(accountDBDir)), "accountDB should have 2 entries")
	require.Equal(t, 2, countLiveEntries(t, s.rawDBFor(codeDBDir)), "codeDB should have 2 entries")

	cs2 := namedCS(storagePair(addr1, slot1, []byte{0xCC}))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)
	require.Equal(t, 2, countLiveEntries(t, s.rawDBFor(storageDBDir)), "overwrite should not increase count")

	cs3 := namedCS(storageDeletePair(addr1, slot1))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)
	require.Equal(t, 1, countLiveEntries(t, s.rawDBFor(storageDBDir)), "delete should decrease count")

	cs4 := namedCS(nonceDeletePair(addr1))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs4}))
	commitAndCheck(t, s)
	require.Equal(t, 2, countLiveEntries(t, s.rawDBFor(accountDBDir)), "account delete should not decrease count")
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
	err := s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs})
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
	err := s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs})
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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(nonceDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(nonceDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(noncePair(addr, 99))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(codeHashDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs0}))
		commitAndCheck(t, s)

		cs1 := namedCS(codeHashDeletePair(addr))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(codeHashPair(addr, codeHashN(0xBB)))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	raw1, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
	require.NoError(t, err)
	ad1, err := vtype.DeserializeAccountData(raw1)
	require.NoError(t, err)
	require.Equal(t, uint64(7), ad1.GetNonce())
	var zeroHash vtype.CodeHash
	require.Equal(t, &zeroHash, ad1.GetCodeHash(), "nonce-only should have zero codehash")

	// Step 2: Add codehash
	cs2 := namedCS(codeHashPair(addr, codeHashN(0xAB)))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	raw2, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
	require.NoError(t, err)
	ad2, err := vtype.DeserializeAccountData(raw2)
	require.NoError(t, err)
	require.Equal(t, uint64(7), ad2.GetNonce(), "nonce should be preserved after codehash write")
	expectedCH := codeHashN(0xAB)
	require.Equal(t, &expectedCH, ad2.GetCodeHash())

	// Step 3: Delete codehash → back to zero codehash
	cs3 := namedCS(codeHashDeletePair(addr))
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)

	raw3, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 42), codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr), codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	_, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 7), codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	raw, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 10)),
	}))
	commitAndCheck(t, s)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addr)),
	}))
	commitAndCheck(t, s)

	_, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
	require.Error(t, err, "row should be deleted after all-zero")

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 99)),
	}))
	commitAndCheck(t, s)

	raw, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 5)),
	}))
	commitAndCheck(t, s)

	// Block 2: write nonce = 0 (write, not delete)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 0)),
	}))
	commitAndCheck(t, s)

	requireFlushedToDisk(t, s)
	_, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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
			require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
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
			require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{namedCS(pairs...)}))
			commitAndCheck(t, s)

			requireFlushedToDisk(t, s)
			_, err := s.rawDBFor(accountDBDir).Get(accountPhysKey(addr))
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1) // should pass: new account, nil old is correct

	// Block 2: update nonce to 2. The account now EXISTS in accountDB with
	// encoded(nonce=1). The buggy code sets oldAccountRawValues[addr] = nil
	// because s.accountWrites is empty after the block-1 commit cleared it.
	// The correct old value is the DB's encoded(nonce=1).
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
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
	require.Equal(t, ver, s.localMeta[miscDBDir].CommittedVersion)
}

func TestApplyChangeSetsNilInput(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := rootHash(s)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, nil))
	require.Equal(t, hashBefore, rootHash(s), "nil input should not change hash")
}

func TestApplyChangeSetsEmptySlice(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := rootHash(s)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{}))
	require.Equal(t, hashBefore, rootHash(s), "empty slice should not change hash")
}

func TestApplyChangeSetsNonEVMModuleRoutesToMisc(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := rootHash(s)

	cs := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("some-bank-key"), Value: []byte("some-value")},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	require.Len(t, s.pendingChangeSets, 1)

	// Physical key in the misc store should be module-prefixed: "bank/some-bank-key"
	physKey := string(ktype.ModulePhysicalKey("bank", []byte("some-bank-key")))
	requireStaged(t, s.miscStore, []byte(physKey),
		"misc store should contain module-prefixed key %q", physKey)

	// Persist and verify round-trip via raw miscDB lookup
	commitAndCheck(t, s)
	require.NotEqual(t, hashBefore, rootHash(s), "misc-routed key changes hash")
	raw, err := s.rawDBFor(miscDBDir).Get([]byte(physKey))
	require.NoError(t, err)
	require.NotNil(t, raw, "miscDB should persist module-prefixed key")
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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{evmCS, bankCS}))

	// EVM storage write should exist.
	requireStaged(t, s.storageStore, ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))

	// The EVM value should be readable via pending writes.
	val, found := s.Get(keys.EVMStoreKey, storageKey)
	require.True(t, found)
	require.Equal(t, padLeft32(0x42), val)

	// Bank key should be in the misc store with module prefix.
	bankPhysKey := string(ktype.ModulePhysicalKey("bank", []byte("bank-key")))
	requireStaged(t, s.miscStore, []byte(bankPhysKey),
		"bank key should be in the misc store with module prefix")
}

func TestApplyChangeSetsEmptyPairsVsNilPairs(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := rootHash(s)

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

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{nilPairsCS, emptyPairsCS}))
	// Nothing to stage, so the working hashes are the only observable, and they must not move.
	require.Equal(t, hashBefore, rootHash(s), "empty changesets must not change the hash")
}

func TestApplyChangeSetsOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	defer ro.Close()

	err = ro.ApplyChangeSets(ro.Version()+1, []*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestApplyChangeSetsInvalidAddressLength(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// A well-formed nonce key: prefix(1) + addr(20) = 21 bytes.
	// Build one manually with correct prefix but wrong addr length.
	// ParseEVMKey checks len(key) != len(noncePrefix)+20 and falls back to misc.
	// To actually trigger "invalid address length" in ApplyChangeSets, we need
	// ParseEVMKey to return EVMKeyNonce with wrong-length keyBytes.
	// This only happens for the correct total length. So instead, test via
	// a key that ParseEVMKey routes to EVMKeyNonce (21 bytes total),
	// but the len(keyBytes) != ktype.AddressLen check in getAccountData fails.
	//
	// Actually, ParseEVMKey always strips the prefix correctly for 21-byte keys.
	// The address will always be 20 bytes. So this error path is unreachable
	// through normal key construction. Instead, verify that malformed nonce keys
	// (wrong total length) are routed to misc.
	truncatedNonceKey := append([]byte{0x0a}, make([]byte, 15)...) // 16 bytes total
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: truncatedNonceKey, Value: nonceBytes(1)},
		}},
	}
	// Routed to EVMKeyMisc (not Nonce), so no address validation error.
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	requireStaged(t, s.miscStore, ktype.ModulePhysicalKey(keys.EVMStoreKey, truncatedNonceKey),
		"malformed nonce key should be treated as misc")
}

func TestApplyChangeSetsErrorRecoveryPartialState(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Seed non-trivial per-module state so a failed Apply is checked against
	// real buckets, not empty maps that would trivially equal a fresh clone.
	seedAddr := addrN(0xAA)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		makeChangeSet(keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(seedAddr, slotN(0x01))), padLeft32(0x11), false),
		{Name: "gov", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("proposal"), Value: []byte{0x01}}}}},
	}))
	commitAndCheck(t, s)
	before := captureWorkingHashes(s)

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

	err := s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid nonce value length")

	// A failed Apply must stage nothing and leave the working lattice state untouched, so a later
	// Commit cannot seal orphaned rows against a stale AppHash. The valid storage pair that preceded
	// the invalid one is the one that would leak.
	requireNotStaged(t, s.storageStore, ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
	requireNotStaged(t, s.accountStore, accountPhysKey(addr))
	require.Empty(t, s.pendingChangeSets)
	require.Equal(t, int64(0), s.pendingBlockHeight)
	requireWorkingHashesUnchanged(t, s, before)

	validCS := makeChangeSet(storageKey, padLeft32(0xBB), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{validCS}))
}

// TestApplyChangeSetsKeepsPendingCleanOnLaterParseError covers the case where
// an earlier DB kind would previously have been maps.Copy'd into the pending
// overlay before a later process*Changes failure — leaving rows that Commit
// could persist while working LtHash still reflected the pre-failure state.
func TestApplyChangeSetsKeepsPendingCleanOnLaterParseError(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	seedAddr := addrN(0xAB)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		makeChangeSet(keys.BuildEVMKey(keys.EVMKeyNonce, seedAddr[:]), nonceBytes(3), false),
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("bal"), Value: []byte{0x02}}}}},
	}))
	commitAndCheck(t, s)
	before := captureWorkingHashes(s)

	addr := addrN(0xCC)
	slot := slotN(0x02)
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: nonceBytes(7)},
			{Key: storageKey, Value: []byte{0x01}}, // not 32 bytes — fails toStorageValues
		}},
	}

	err := s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse storage changes")

	requireNotStaged(t, s.accountStore, accountPhysKey(addr),
		"account rows must not stage before storage validation finishes")
	require.Empty(t, s.pendingChangeSets)
	require.Equal(t, int64(0), s.pendingBlockHeight)
	requireWorkingHashesUnchanged(t, s, before)

	// A subsequent Commit must not invent on-disk state for the failed apply.
	// A successful apply stages every row, so absence from
	// pending is not enough — read the keys back and check committedLtHash
	// (the AppHash input) stayed put.
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.True(t, s.committedLtHash.Equal(before.global))
	_, ok := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.False(t, ok, "nonce row from the failed apply must not be persisted")
	_, ok = s.Get(keys.EVMStoreKey, storageKey)
	require.False(t, ok, "storage row from the failed apply must not be persisted")
}

// TestCommitFailsCleanlyOnHashError pins that a hash failure does not leave the store believing it
// committed.
func TestCommitFailsCleanlyOnHashError(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	seedAddr := addrN(0xAC)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		makeChangeSet(keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(seedAddr, slotN(0x09))), padLeft32(0x99), false),
		{Name: "gov", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("params"), Value: []byte{0x03}}}}},
	}))
	commitAndCheck(t, s)
	committed := s.Version()
	before := captureWorkingHashes(s)

	s.ltCalc = lthash.NewHashCalculator(s.ltHashPool, dataDBDirs, func([]byte) (string, error) {
		return "", fmt.Errorf("injected moduleOf failure")
	})

	addr := addrN(0xDD)
	slot := slotN(0x03)
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		makeChangeSet(storageKey, padLeft32(0xEE), false),
	}))

	_, err := s.Commit(s.Version() + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected moduleOf failure")

	// The store must not look like the block landed.
	require.Equal(t, committed, s.Version(), "a failed commit must not advance the version")
	requireWorkingHashesUnchanged(t, s, before)
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
	require.Error(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
}

func TestApplyChangeSetsNonPrefixedKeyGoesToMisc(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := rootHash(s)

	// A key with an unrecognized prefix goes to EVMKeyMisc, not skipped.
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{0xFF, 0x01, 0x02}, Value: []byte{0xAA}},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)
	require.NotEqual(t, hashBefore, rootHash(s), "misc key changes hash")
}

func TestCommitWithoutPriorApply(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := rootHash(s)

	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
	require.Equal(t, hashBefore, rootHash(s), "hash should be unchanged after empty commit")
}

func TestDoubleCommitNoApplyBetween(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	v1, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v1)
	hashAfterV1 := rootHash(s)

	// Second commit with no new apply.
	v2, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), v2)
	require.Equal(t, hashAfterV1, rootHash(s), "hash unchanged between commits without apply")
}

func TestCommitOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	defer ro.Close()

	_, err = ro.Commit(ro.Version() + 1)
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestCommitRejectsVersionNotAhead(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	_, err := s.Commit(0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "committing bad version")
	require.Equal(t, int64(0), s.Version())

	// Empty commits may not jump ahead either: the first block is 1.
	_, err = s.Commit(5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "committing bad version")
	require.Equal(t, int64(0), s.Version())

	v, err := s.Commit(1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v)

	// Committing the same block again reports the same result and changes nothing. Cosmos does this
	// on every block: RootHash commits, then rootmulti calls Commit for the block already committed.
	v, err = s.Commit(1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
	require.Equal(t, int64(1), s.Version())

	// Going backwards is still rejected.
	_, err = s.Commit(0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "committing bad version")
}

// TestRejectedCommitLeavesStoreIntact pins that a commit at the wrong version changes nothing: the version
// does not advance and the buffered writes survive, so the correct commit still works afterwards.
func TestRejectedCommitLeavesStoreIntact(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets(1, []*proto.NamedChangeSet{cs}))

	_, err := s.Commit(5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "committing bad version")
	require.Equal(t, int64(0), s.Version(), "rejected commit must not advance version")
	requireStaged(t, s.storageStore,
		ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01))),
		"rejected commit must leave the staged row intact")

	v, err := s.Commit(1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
}

// TestApplyChangeSetsAllowsSameHeightRepeatsOnly pins the one-block-per-commit
// contract: a single block's writes may arrive across several ApplyChangeSets
// calls at the same height, but any other height is rejected, in either
// direction. Batching several blocks before one Commit is not supported —
// changesets carry no block number, so the batch would collapse into a single WAL
// entry at its highest height, leaving the skipped heights unreplayable.
func TestApplyChangeSetsAllowsSameHeightRepeatsOnly(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key1 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	key2 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x02)))

	// Same-height repeat: splitting one block's writes across two calls.
	require.NoError(t, s.ApplyChangeSets(1, []*proto.NamedChangeSet{makeChangeSet(key1, padLeft32(0x11), false)}))
	require.NoError(t, s.ApplyChangeSets(1, []*proto.NamedChangeSet{makeChangeSet(key2, padLeft32(0x22), false)}))
	require.Equal(t, int64(1), s.PendingVersion())

	// Advancing to the next block before committing this one is rejected.
	err := s.ApplyChangeSets(2, []*proto.NamedChangeSet{makeChangeSet(key1, padLeft32(0x33), false)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "plus one")

	// So is going backwards.
	err = s.ApplyChangeSets(0, []*proto.NamedChangeSet{makeChangeSet(key1, padLeft32(0x44), false)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "plus one")

	v, err := s.Commit(1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
	require.Equal(t, int64(0), s.PendingVersion())

	for _, key := range [][]byte{key1, key2} {
		height, found, err := s.GetBlockHeightModified(keys.EVMStoreKey, key)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(1), height)
	}
}

func TestCommitStateChangesStampsRowBlockHeight(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)

	// Start at block 10 the legal way: seed the store so its history begins there.
	require.NoError(t, s.SetInitialVersion(10))
	require.NoError(t, s.CommitStateChanges(10, []*proto.NamedChangeSet{cs}))
	require.Equal(t, int64(10), s.Version())

	height, found, err := s.GetBlockHeightModified(keys.EVMStoreKey, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(10), height)
}

func TestCommitVersionMonotonicAfterMultipleEmptyCommits(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	for i := int64(1); i <= 5; i++ {
		v, err := s.Commit(s.Version() + 1)
		require.NoError(t, err)
		require.Equal(t, i, v)
	}
	require.Equal(t, int64(5), s.Version())
}

func TestNonEVMModuleKeyRoundTrip(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	err := s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
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
	_, err = s.Commit(s.Version() + 1)
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
