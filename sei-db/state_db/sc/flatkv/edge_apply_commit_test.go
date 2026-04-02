package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

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

	// ParseEVMKey routes any non-empty key that doesn't match a known prefix
	// to EVMKeyLegacy. The module Name field is NOT used as a filter.
	cs := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("some-bank-key"), Value: []byte("some-value")},
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	// The key is treated as a legacy EVM key, so hash changes.
	require.NotEqual(t, hashBefore, s.RootHash(), "legacy-routed key changes hash")
	require.Len(t, s.legacyWrites, 1)
	require.Len(t, s.storageWrites, 0)
	require.Len(t, s.pendingChangeSets, 1)
}

func TestApplyChangeSetsMixedEVMAndNonEVM(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0xAA)
	slot := slotN(0x01)
	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	evmCS := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: []byte{0x42}},
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
	val, found := s.Get(storageKey)
	require.True(t, found)
	require.Equal(t, []byte{0x42}, val)
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
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x11}, false)
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
	// but AddressFromBytes() fails because keyBytes are manipulated.
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
	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	// First pair: valid storage write
	// Second pair: invalid nonce length (triggers error)
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: []byte{0xAA}},
			{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]), Value: []byte{0x01, 0x02}}, // wrong length
		}},
	}

	err := s.ApplyChangeSets([]*proto.NamedChangeSet{cs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid nonce value length")

	// The storage write may have been buffered before the error.
	// Verify the store doesn't panic and can still accept new operations.
	validCS := makeChangeSet(storageKey, []byte{0xBB}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{validCS}))
}

func TestApplyChangeSetsEVMKeyEmptySkipped(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	hashBefore := s.RootHash()

	// Only zero-length keys return EVMKeyUnknown (alias for EVMKeyEmpty).
	// All non-empty keys are routed to at least EVMKeyLegacy.
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{}, Value: []byte{0xAA}},
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	require.Equal(t, hashBefore, s.RootHash(), "empty key should be silently skipped")
	require.Len(t, s.legacyWrites, 0)
	require.Len(t, s.storageWrites, 0)
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
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x11}, false)
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
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x11}, false)
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
