package flatkv

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers
// =============================================================================

// evmStorageKey builds a prefix-encoded storage key for the external Get/Has API.
func evmStorageKey(addr ktype.Address, slot ktype.Slot) []byte {
	internal := ktype.StorageKey(addr, slot)
	return keys.BuildEVMKey(keys.EVMKeyStorage, internal)
}

// accountPhysKey returns the physical DB key for an account address.
func accountPhysKey(addr ktype.Address) []byte {
	return ktype.EVMPhysicalKey(ktype.EVMKeyAccount, addr[:])
}

// storagePhysKey returns the physical DB key for a storage slot.
func storagePhysKey(addr ktype.Address, slot ktype.Slot) []byte {
	return ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
}

// padLeft32 returns a 32-byte big-endian value with the given bytes right-aligned.
func padLeft32(val ...byte) []byte {
	var b [32]byte
	copy(b[32-len(val):], val)
	return b[:]
}

// makeChangeSet creates a changeset
func makeChangeSet(key, value []byte, delete bool) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: key, Value: value, Delete: delete},
			},
		},
	}
}

// setupTestDB creates a temporary PebbleDB for testing
func setupTestDB(t *testing.T) types.KeyValueDB {
	t.Helper()
	cfg := pebbledb.DefaultTestConfig(t)
	cacheCfg := pebbledb.DefaultTestCacheConfig()
	db, err := pebbledb.OpenWithCache(t.Context(), &cfg, &cacheCfg,
		threading.NewAdHocPool(), threading.NewAdHocPool())
	require.NoError(t, err)
	return db
}

// setupTestStore creates a minimal test store
func setupTestStore(t *testing.T) *CommitStore {
	t.Helper()
	s, err := NewCommitStore(t.Context(), config.DefaultTestConfig(t))
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	return s
}

// setupTestStoreWithConfig creates a test store with custom config
func setupTestStoreWithConfig(t *testing.T, cfg *config.Config) *CommitStore {
	t.Helper()
	dir := t.TempDir()
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	return s
}

// commitAndCheck commits and asserts no error, returns the version
func commitAndCheck(t *testing.T, s *CommitStore) int64 {
	t.Helper()
	v, err := s.Commit()
	require.NoError(t, err)
	return v
}

// ---------- helpers to build prefix-encoded changeset pairs ----------
func nonceBytes(n uint64) []byte {
	b := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func addrN(n byte) ktype.Address {
	var a ktype.Address
	a[19] = n
	return a
}

func slotN(n byte) ktype.Slot {
	var s ktype.Slot
	s[31] = n
	return s
}

func codeHashN(n byte) vtype.CodeHash {
	var h vtype.CodeHash
	for i := range h {
		h[i] = n
	}
	return h
}

func noncePair(addr ktype.Address, nonce uint64) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		Value: nonceBytes(nonce),
	}
}

func codeHashPair(addr ktype.Address, ch vtype.CodeHash) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
		Value: ch[:],
	}
}

func codePair(addr ktype.Address, bytecode []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
		Value: bytecode,
	}
}

func codeDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
		Delete: true,
	}
}

func storagePair(addr ktype.Address, slot ktype.Slot, val []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
		Value: padLeft32(val...),
	}
}

func storageDeletePair(addr ktype.Address, slot ktype.Slot) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
		Delete: true,
	}
}

func nonceDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		Delete: true,
	}
}

func codeHashDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
		Delete: true,
	}
}

func namedCS(pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
}

// CountKeys returns the total number of non-meta keys across all data DBs in s.
// It uses RawGlobalIterator, so pending (uncommitted) writes are not counted.
func CountKeys(s *CommitStore) (int64, error) {
	iter, err := s.RawGlobalIterator()
	if err != nil {
		return 0, err
	}
	defer func() { _ = iter.Close() }()
	var count int64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	if err := iter.Error(); err != nil {
		return 0, err
	}
	return count, nil
}
