package flatkv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
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
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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

	require.NoError(t, imp.AddModule(evm.EVMFlatKVStoreKey))
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
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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

func TestImportSurvivesReopen(t *testing.T) {
	src := setupTestStore(t)
	defer src.Close()

	addr := Address{0xDD}
	slot := Slot{0xEE}

	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	storageVal := []byte{0xFF}
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 7}

	require.NoError(t, src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: storageVal},
			{Key: nonceKey, Value: nonceVal},
		}}},
	}))
	commitAndCheck(t, src)
	srcHash := src.RootHash()

	exp, err := src.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	// Import into a fresh store at a known directory.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, flatkvRootDir)

	cfg := DefaultTestConfig(t)
	cfg.DataDir = dbPath

	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s1.Importer(1)
	require.NoError(t, err)
	require.NoError(t, imp.AddModule(evm.EVMFlatKVStoreKey))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())
	require.NoError(t, s1.Close())

	// Reopen from the same directory — data must survive.
	cfg2 := DefaultTestConfig(t)
	cfg2.DataDir = dbPath

	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(1, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())

	got, found := s2.Get(storageKey)
	require.True(t, found, "storage key must survive reopen")
	require.Equal(t, storageVal, got)

	got, found = s2.Get(nonceKey)
	require.True(t, found, "nonce key must survive reopen")
	require.Equal(t, nonceVal, got)

	require.Equal(t, srcHash, s2.RootHash())
}

// TestImportPurgesStaleData verifies that importing a snapshot into a store
// that already contains data removes keys not present in the snapshot.
// Covers all four DB types: storage, account (nonce/codehash), code, and
// ensures stale keys from every DB are purged.
func TestImportPurgesStaleData(t *testing.T) {
	// --- Phase 1: populate a store with data across all DB types ---
	dir := t.TempDir()
	dbPath := filepath.Join(dir, flatkvRootDir)

	cfg := DefaultTestConfig(t)
	cfg.DataDir = dbPath

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addrA := Address{0xAA}
	addrB := Address{0xBB}
	addrStale := Address{0xCC} // will be absent from the imported snapshot
	slotA := Slot{0x01}
	slotStale := Slot{0x03}

	// Storage keys
	storageA := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrA, slotA))
	storageStale := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrStale, slotStale))
	// Account keys (nonce + codehash)
	nonceA := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addrA[:])
	nonceStale := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addrStale[:])
	codeHashB := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addrB[:])
	codeHashStale := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addrStale[:])
	// Code key
	codeB := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addrB[:])
	codeStale := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addrStale[:])

	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	codeHashVal := make([]byte, CodeHashLen)
	codeHashVal[31] = 0xAB
	codeVal := []byte{0x60, 0x80}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageA, Value: []byte{0x0A}},
			{Key: storageStale, Value: []byte{0x0C}},
			{Key: nonceA, Value: nonceVal},
			{Key: nonceStale, Value: nonceVal},
			{Key: codeHashB, Value: codeHashVal},
			{Key: codeHashStale, Value: codeHashVal},
			{Key: codeB, Value: codeVal},
			{Key: codeStale, Value: codeVal},
		}}},
	}))
	commitAndCheck(t, s)

	staleKeys := [][]byte{storageStale, nonceStale, codeHashStale, codeStale}

	for _, k := range staleKeys {
		_, found := s.Get(k)
		require.True(t, found, "pre-import: key should exist")
	}

	// --- Phase 2: build a snapshot that only contains addrA/addrB data ---
	src := setupTestStore(t)
	defer src.Close()

	newStorageVal := []byte{0xA1}
	newNonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 5}
	newCodeHashVal := make([]byte, CodeHashLen)
	newCodeHashVal[31] = 0xCD
	newCodeVal := []byte{0x60, 0x40, 0x52}

	require.NoError(t, src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageA, Value: newStorageVal},
			{Key: nonceA, Value: newNonceVal},
			{Key: codeHashB, Value: newCodeHashVal},
			{Key: codeB, Value: newCodeVal},
		}}},
	}))
	commitAndCheck(t, src)
	srcHash := src.RootHash()

	exp, err := src.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	// --- Phase 3: import snapshot into the existing store ---
	require.NoError(t, s.Close())

	s, err = NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)
	require.NoError(t, imp.AddModule(evm.EVMFlatKVStoreKey))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	// --- Phase 4: verify stale keys are gone across all DB types ---
	got, found := s.Get(storageA)
	require.True(t, found, "storage key A should exist")
	require.Equal(t, newStorageVal, got)

	got, found = s.Get(nonceA)
	require.True(t, found, "nonce key A should exist")
	require.Equal(t, newNonceVal, got)

	got, found = s.Get(codeB)
	require.True(t, found, "code key B should exist")
	require.Equal(t, newCodeVal, got)

	got, found = s.Get(codeHashB)
	require.True(t, found, "codehash key B should exist")
	require.Equal(t, newCodeHashVal, got)

	for _, k := range staleKeys {
		_, found = s.Get(k)
		require.False(t, found, "stale key should NOT exist after import")
	}

	require.Equal(t, srcHash, s.RootHash(), "LtHash must match source after clean import")

	// Verify the store survives a reopen.
	require.NoError(t, s.Close())
	s, err = NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(1, false)
	require.NoError(t, err)
	defer s.Close()

	require.Equal(t, int64(1), s.Version())
	for _, k := range staleKeys {
		_, found = s.Get(k)
		require.False(t, found, "stale key must remain absent after reopen")
	}
	require.Equal(t, srcHash, s.RootHash())
}

func TestImporterFailsWhenResetCannotRemoveCurrentLink(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, flatkvRootDir)

	cfg := DefaultTestConfig(t)
	cfg.DataDir = dbPath

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	current := currentPath(s.flatkvDir())
	err = os.Remove(current)
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	require.NoError(t, os.Mkdir(current, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(current, "sentinel"), []byte("blocked"), 0o600))

	imp, err := s.Importer(1)
	require.Nil(t, imp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reset store for import")
	require.Contains(t, err.Error(), "remove "+currentLink)

	info, statErr := os.Stat(current)
	require.NoError(t, statErr)
	require.True(t, info.IsDir(), "failed reset must not proceed past the invalid current path")
}

func TestImporterOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		[]byte{0x11}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	_, err = ro.Importer(1)
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestImporterHeightNonZeroSkipped(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	// Non-leaf nodes (Height != 0) are silently skipped.
	imp.AddNode(&types.SnapshotNode{
		Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		Value:  []byte{0x11},
		Height: 1, // non-leaf
	})

	require.NoError(t, imp.Close())

	// Data should NOT have been imported.
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01)))
	_, found := s.Get(key)
	require.False(t, found, "height != 0 node should be skipped")
	require.NoError(t, s.Close())
}

func TestImporterNilKeySkipped(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	// Nodes with nil key are silently skipped.
	imp.AddNode(&types.SnapshotNode{
		Key:    nil,
		Value:  []byte{0xAA},
		Height: 0,
	})

	require.NoError(t, imp.Close())
	require.Equal(t, int64(1), s.Version())
	require.NoError(t, s.Close())
}

func TestImporterEmptyStore(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(5)
	require.NoError(t, err)

	// Import zero nodes.
	require.NoError(t, imp.Close())

	require.Equal(t, int64(5), s.Version())
	require.NoError(t, s.Close())
}

func TestImporterCorruptKeyDataPropagatesError(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	// Add a valid storage node first.
	imp.AddNode(&types.SnapshotNode{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		Value: []byte{0x11},
	})

	// Add a node with a nonce key but invalid nonce value length.
	// This should cause ApplyChangeSets to error during flush/close.
	addr2 := addrN(0x02)
	imp.AddNode(&types.SnapshotNode{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr2[:]),
		Value: []byte{0x01, 0x02}, // wrong length for nonce (needs 8 bytes)
	})

	err = imp.Close()
	require.Error(t, err, "import with invalid nonce length should fail")
	// Don't close s here -- it may be in a partial state; just let test cleanup handle it.
}

func TestImporterDoubleImport(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	// First import.
	imp1, err := s.Importer(1)
	require.NoError(t, err)
	imp1.AddNode(&types.SnapshotNode{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		Value: []byte{0x11},
	})
	require.NoError(t, imp1.Close())

	key1 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01)))
	val, found := s.Get(key1)
	require.True(t, found)
	require.Equal(t, []byte{0x11}, val)

	// Second import: should wipe prior state (resetForImport).
	imp2, err := s.Importer(2)
	require.NoError(t, err)
	imp2.AddNode(&types.SnapshotNode{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x02), slotN(0x02))),
		Value: []byte{0x22},
	})
	require.NoError(t, imp2.Close())

	require.Equal(t, int64(2), s.Version())

	// Data from first import should be gone.
	_, found = s.Get(key1)
	require.False(t, found, "first import data should be wiped by second import")

	key2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x02), slotN(0x02)))
	val, found = s.Get(key2)
	require.True(t, found)
	require.Equal(t, []byte{0x22}, val)
	require.NoError(t, s.Close())
}

func TestExporterAtHistoricalVersion(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x10)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))

	// v1: write 0x11
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// v2: write 0x22
	cs2 := makeChangeSet(key, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// v3: write 0x33
	cs3 := makeChangeSet(key, []byte{0x33}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)

	// Export at v1 (historical).
	exp, err := s.Exporter(1)
	require.NoError(t, err)

	var storageNodes []*types.SnapshotNode
	for {
		item, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone))
			break
		}
		node := item.(*types.SnapshotNode)
		kind, _ := evm.ParseEVMKey(node.Key)
		if kind == evm.EVMKeyStorage {
			storageNodes = append(storageNodes, node)
		}
	}
	require.NoError(t, exp.Close())

	require.Len(t, storageNodes, 1)
	require.Equal(t, []byte{0x11}, storageNodes[0].Value, "historical export should have v1 value")
}

func TestExportImportLargerDataset(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 5
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	// Write multiple key types across multiple addresses.
	for i := byte(1); i <= 10; i++ {
		addr := addrN(i)
		pairs := []*proto.KVPair{
			noncePair(addr, uint64(i)),
			{
				Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(i))),
				Value: []byte{i, i, i},
			},
		}
		if i%3 == 0 {
			pairs = append(pairs,
				codeHashPair(addr, codeHashN(i)),
				codePair(addr, []byte{0x60, i}),
			)
		}
		cs := &proto.NamedChangeSet{
			Name:      "evm",
			Changeset: proto.ChangeSet{Pairs: pairs},
		}
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	originalHash := s.RootHash()

	// Export.
	exp, err := s.Exporter(0)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Greater(t, len(nodes), 0)

	// Import into a fresh store.
	dir2 := t.TempDir()
	cfg2 := DefaultTestConfig(t)
	cfg2.DataDir = filepath.Join(dir2, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s2.Importer(10)
	require.NoError(t, err)
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	require.Equal(t, int64(10), s2.Version())
	require.Equal(t, originalHash, s2.RootHash(), "imported store should have identical RootHash")
	require.NoError(t, s2.Close())
}

func TestExporterCorruptAccountValueInDB(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x20)
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 42),
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Corrupt the account value in accountDB with invalid-length data.
	batch := s.accountDB.NewBatch()
	require.NoError(t, batch.Set(AccountKey(addr), []byte{0xDE, 0xAD}))
	require.NoError(t, batch.Commit(dbtypes.WriteOptions{Sync: true}))
	_ = batch.Close()

	// Construct an exporter directly on this store to exercise the
	// corrupt-account path without the read-only checkpoint (which
	// replays the WAL and restores the clean value).
	exp := NewKVExporter(s, s.Version())

	var hitError bool
	for {
		_, err := exp.Next()
		if err != nil {
			if errors.Is(err, errorutils.ErrorExportDone) {
				break
			}
			require.Contains(t, err.Error(), "corrupt account entry")
			hitError = true
			break
		}
	}
	require.True(t, hitError, "exporter should return error on corrupt AccountValue")
	// Only close the iterator, not the underlying store (we own s via defer).
	if exp.currentIter != nil {
		_ = exp.currentIter.Close()
	}
}
