package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/stretchr/testify/require"
)

func TestPerDBLtHashPartialKeyTypeOperations(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))

	// Write only storage keys: other DBs' per-DB LtHash should remain zero.
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	zeroChecksum := lthash.New().Checksum()
	require.NotEqual(t, zeroChecksum, s.perDBWorkingLtHash[storageDBDir].Checksum(),
		"storageDB hash should be non-zero")
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[accountDBDir].Checksum(),
		"accountDB hash should remain zero")
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[codeDBDir].Checksum(),
		"codeDB hash should remain zero")
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[legacyDBDir].Checksum(),
		"legacyDB hash should remain zero")
}

func TestPerDBLtHashDeleteLastKeyZerosHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x02)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))

	cs := makeChangeSet(key, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	nonZeroHash := s.perDBWorkingLtHash[storageDBDir].Checksum()
	zeroChecksum := lthash.New().Checksum()
	require.NotEqual(t, zeroChecksum, nonZeroHash)

	// Delete the only storage key.
	delCS := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{delCS}))
	commitAndCheck(t, s)

	// After deleting all keys from a DB, its hash should return to zero.
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[storageDBDir].Checksum(),
		"storageDB hash should be zero after deleting all keys")

	// Verify via full scan.
	scanHash := testFullScanDBLtHash(t, s.storageDB)
	require.Equal(t, zeroChecksum, scanHash.Checksum())
}

func TestPerDBLtHashSumInvariantAcrossAllOperations(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	verifySumInvariant := func(msg string) {
		t.Helper()
		globalHash := lthash.New()
		for _, dir := range dataDBDirs {
			globalHash.MixIn(s.perDBWorkingLtHash[dir])
		}
		require.Equal(t, s.workingLtHash.Checksum(), globalHash.Checksum(),
			"sum(perDB) should equal global workingLtHash: %s", msg)
	}

	addr := addrN(0x03)

	// Operation 1: Add storage key.
	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(storageKey, []byte{0x33}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)
	verifySumInvariant("after storage add")

	// Operation 2: Add account fields.
	cs2 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 10),
			codeHashPair(addr, codeHashN(0xAA)),
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)
	verifySumInvariant("after account add")

	// Operation 3: Add code.
	cs3 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			codePair(addr, []byte{0x60, 0x60}),
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)
	verifySumInvariant("after code add")

	// Operation 4: Update storage.
	cs4 := makeChangeSet(storageKey, []byte{0x44}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs4}))
	commitAndCheck(t, s)
	verifySumInvariant("after storage update")

	// Operation 5: Delete storage.
	cs5 := makeChangeSet(storageKey, nil, true)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs5}))
	commitAndCheck(t, s)
	verifySumInvariant("after storage delete")

	// Operation 6: Delete account (nonce + codehash).
	cs6 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]), Delete: true},
			{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs6}))
	commitAndCheck(t, s)
	verifySumInvariant("after account delete")

	// Operation 7: Delete code.
	cs7 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs7}))
	commitAndCheck(t, s)
	verifySumInvariant("after code delete")

	// Operation 8: Empty commit.
	commitAndCheck(t, s)
	verifySumInvariant("after empty commit")
}
