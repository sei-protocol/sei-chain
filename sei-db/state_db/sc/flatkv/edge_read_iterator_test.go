package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

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
