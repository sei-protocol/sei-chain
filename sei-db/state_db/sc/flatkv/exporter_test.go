package flatkv

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

// drainExporter collects all SnapshotNode items from an exporter.
func drainExporter(t *testing.T, exp types.Exporter) []*types.SnapshotNode {
	t.Helper()
	var nodes []*types.SnapshotNode
	for {
		item, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone))
			break
		}
		node, ok := item.(*types.SnapshotNode)
		require.True(t, ok, "expected *SnapshotNode, got %T", item)
		nodes = append(nodes, node)
	}
	return nodes
}

func TestExporterEmptyStore(t *testing.T) {
	s := setupTestStore(t)

	exp, err := s.Exporter(0)
	require.NoError(t, err)
	defer exp.Close()

	_, err = exp.Next()
	require.True(t, errors.Is(err, errorutils.ErrorExportDone))
	require.NoError(t, s.Close())
}

func TestExporterStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	slot1 := Slot{0x01}
	slot2 := Slot{0x02}
	val1 := []byte{0x11}
	val2 := []byte{0x22}

	key1 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot1))
	key2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot2))

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: key1, Value: val1},
			{Key: key2, Value: val2},
		}}},
	}))
	commitAndCheck(t, s)

	exp, err := s.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	require.Len(t, nodes, 2)
	for _, n := range nodes {
		require.Equal(t, int64(1), n.Version)
		require.Equal(t, int8(0), n.Height)
		kind, _ := evm.ParseEVMKey(n.Key)
		require.Equal(t, evm.EVMKeyStorage, kind)
	}
}

func TestExporterAccountKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xBB}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 42}

	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	codeHashVal := make([]byte, CodeHashLen)
	codeHashVal[0] = 0xDE

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: nonceKey, Value: nonceVal},
			{Key: codeHashKey, Value: codeHashVal},
		}}},
	}))
	commitAndCheck(t, s)

	exp, err := s.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	// accountDB produces nonce + codehash nodes per account
	require.Len(t, nodes, 2)

	kindMap := map[evm.EVMKeyKind]*types.SnapshotNode{}
	for _, n := range nodes {
		kind, _ := evm.ParseEVMKey(n.Key)
		kindMap[kind] = n
	}

	require.Contains(t, kindMap, evm.EVMKeyNonce)
	require.Equal(t, nonceVal, kindMap[evm.EVMKeyNonce].Value)

	require.Contains(t, kindMap, evm.EVMKeyCodeHash)
	require.Equal(t, codeHashVal, kindMap[evm.EVMKeyCodeHash].Value)
}

func TestExporterCodeKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xCC}
	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	codeVal := []byte{0x60, 0x80, 0x60, 0x40}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: codeKey, Value: codeVal},
		}}},
	}))
	commitAndCheck(t, s)

	exp, err := s.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	var codeNodes []*types.SnapshotNode
	for _, n := range nodes {
		kind, _ := evm.ParseEVMKey(n.Key)
		if kind == evm.EVMKeyCode {
			codeNodes = append(codeNodes, n)
		}
	}
	require.Len(t, codeNodes, 1)
	require.Equal(t, codeVal, codeNodes[0].Value)
}

func TestExporterRoundTrip(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xDD}
	slot := Slot{0xEE}

	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	storageVal := []byte{0xFF}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 7}
	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	codeVal := []byte{0x60, 0x80}
	codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	codeHashVal := make([]byte, CodeHashLen)
	codeHashVal[31] = 0xAB

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: storageKey, Value: storageVal},
			{Key: nonceKey, Value: nonceVal},
			{Key: codeKey, Value: codeVal},
			{Key: codeHashKey, Value: codeHashVal},
		}}},
	}))
	commitAndCheck(t, s)

	srcHash := s.RootHash()

	// --- Export ---
	exp, err := s.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Greater(t, len(nodes), 0)

	// --- Import into fresh store ---
	s2 := setupTestStore(t)
	imp, err := s2.Importer(1)
	require.NoError(t, err)

	require.NoError(t, imp.AddModule("evm_flatkv"))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	// --- Verify round-trip ---
	require.Equal(t, int64(1), s2.Version())

	got, found := s2.Get(storageKey)
	require.True(t, found, "storage key should exist after import")
	require.Equal(t, storageVal, got)

	got, found = s2.Get(nonceKey)
	require.True(t, found, "nonce key should exist after import")
	require.Equal(t, nonceVal, got)

	got, found = s2.Get(codeKey)
	require.True(t, found, "code key should exist after import")
	require.Equal(t, codeVal, got)

	got, found = s2.Get(codeHashKey)
	require.True(t, found, "codehash key should exist after import")
	require.Equal(t, codeHashVal, got)

	// LtHash should match source since import recomputes it via ApplyChangeSets
	require.Equal(t, srcHash, s2.RootHash())

	require.NoError(t, s2.Close())
}

func TestExporterReadOnlyGuard(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	_, err = ro.Exporter(1)
	require.ErrorIs(t, err, errReadOnly)
}

func TestExporterEOAAccountOmitsCodeHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 1}

	// EOA: only nonce, no codehash
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: nonceKey, Value: nonceVal},
		}}},
	}))
	commitAndCheck(t, s)

	exp, err := s.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	// EOA should only produce a nonce node (no codehash)
	require.Len(t, nodes, 1)
	kind, _ := evm.ParseEVMKey(nodes[0].Key)
	require.Equal(t, evm.EVMKeyNonce, kind)
	require.Equal(t, nonceVal, nodes[0].Value)
}
