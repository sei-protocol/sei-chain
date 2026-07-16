package flatkv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// drainExporter collects all SnapshotNode items from an exporter, skipping
// the leading keys.FlatKVStoreKey module header (a string) that the
// self-describing exporter emits before its nodes.
func drainExporter(t *testing.T, exp types.Exporter) []*types.SnapshotNode {
	t.Helper()
	var nodes []*types.SnapshotNode
	for {
		item, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone))
			break
		}
		if name, ok := item.(string); ok {
			require.Equal(t, keys.FlatKVStoreKey, name, "unexpected module header")
			continue
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

	// Even an empty store emits its module header first, then ErrorExportDone.
	item, err := exp.Next()
	require.NoError(t, err)
	require.Equal(t, keys.FlatKVStoreKey, item)

	_, err = exp.Next()
	require.True(t, errors.Is(err, errorutils.ErrorExportDone))
	require.NoError(t, s.Close())
}

func TestExporterStorageKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xAA}
	slot1 := ktype.Slot{0x01}
	slot2 := ktype.Slot{0x02}
	val1 := padLeft32(0x11)
	val2 := padLeft32(0x22)

	key1 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot1))
	key2 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot2))

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
		kind, _, err := ktype.StripEVMPhysicalKey(n.Key)
		require.NoError(t, err)
		require.Equal(t, keys.EVMKeyStorage, kind)
	}
}

func TestExporterAccountKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xBB}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 42}

	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	codeHashVal := make([]byte, vtype.CodeHashLen)
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

	// nonce + codehash merge into a single account row in accountDB
	require.Len(t, nodes, 1)

	n := nodes[0]
	kind, _, err := ktype.StripEVMPhysicalKey(n.Key)
	require.NoError(t, err)
	require.Equal(t, ktype.EVMKeyAccount, kind)

	acct, err := vtype.DeserializeAccountData(n.Value)
	require.NoError(t, err)
	require.Equal(t, uint64(42), acct.GetNonce())
	require.Equal(t, byte(0xDE), acct.GetCodeHash()[0])
}

func TestExporterCodeKeys(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xCC}
	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
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

	require.Len(t, nodes, 1)
	kind, _, err := ktype.StripEVMPhysicalKey(nodes[0].Key)
	require.NoError(t, err)
	require.Equal(t, keys.EVMKeyCode, kind)

	code, err := vtype.DeserializeCodeData(nodes[0].Value)
	require.NoError(t, err)
	require.Equal(t, codeVal, code.GetBytecode())
}

func TestExporterRoundTrip(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xDD}
	slot := ktype.Slot{0xEE}

	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	storageVal := padLeft32(0xFF)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 7}
	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	codeVal := []byte{0x60, 0x80}
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	codeHashVal := make([]byte, vtype.CodeHashLen)
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

	require.NoError(t, imp.AddModule("flatkv"))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	// --- Verify round-trip ---
	require.Equal(t, int64(1), s2.Version())

	got, found := s2.Get(keys.EVMStoreKey, storageKey)
	require.True(t, found, "storage key should exist after import")
	require.Equal(t, storageVal, got)

	got, found = s2.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce key should exist after import")
	require.Equal(t, nonceVal, got)

	got, found = s2.Get(keys.EVMStoreKey, codeKey)
	require.True(t, found, "code key should exist after import")
	require.Equal(t, codeVal, got)

	got, found = s2.Get(keys.EVMStoreKey, codeHashKey)
	require.True(t, found, "codehash key should exist after import")
	require.Equal(t, codeHashVal, got)

	// LtHash should match because import recomputes it from the same physical key/value pairs
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

	addr := ktype.Address{0xAA}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
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

	// EOA produces a single account node with zero codehash (compact form)
	require.Len(t, nodes, 1)
	kind, _, err := ktype.StripEVMPhysicalKey(nodes[0].Key)
	require.NoError(t, err)
	require.Equal(t, ktype.EVMKeyAccount, kind)

	acct, err := vtype.DeserializeAccountData(nodes[0].Value)
	require.NoError(t, err)
	require.Equal(t, uint64(1), acct.GetNonce())
	var zeroHash vtype.CodeHash
	require.Equal(t, &zeroHash, acct.GetCodeHash())
}

func TestImportSurvivesReopen(t *testing.T) {
	src := setupTestStore(t)
	defer src.Close()

	addr := ktype.Address{0xDD}
	slot := ktype.Slot{0xEE}

	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	storageVal := padLeft32(0xFF)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbPath

	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s1.Importer(1)
	require.NoError(t, err)
	require.NoError(t, imp.AddModule("flatkv"))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())
	require.NoError(t, s1.Close())

	// Reopen from the same directory — data must survive.
	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbPath

	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(1, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())

	got, found := s2.Get(keys.EVMStoreKey, storageKey)
	require.True(t, found, "storage key must survive reopen")
	require.Equal(t, storageVal, got)

	got, found = s2.Get(keys.EVMStoreKey, nonceKey)
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbPath

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addrA := ktype.Address{0xAA}
	addrB := ktype.Address{0xBB}
	addrStale := ktype.Address{0xCC} // will be absent from the imported snapshot
	slotA := ktype.Slot{0x01}
	slotStale := ktype.Slot{0x03}

	// Storage keys
	storageA := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrA, slotA))
	storageStale := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrStale, slotStale))
	// Account keys (nonce + codehash)
	nonceA := keys.BuildEVMKey(keys.EVMKeyNonce, addrA[:])
	nonceStale := keys.BuildEVMKey(keys.EVMKeyNonce, addrStale[:])
	codeHashB := keys.BuildEVMKey(keys.EVMKeyCodeHash, addrB[:])
	codeHashStale := keys.BuildEVMKey(keys.EVMKeyCodeHash, addrStale[:])
	// Code key
	codeB := keys.BuildEVMKey(keys.EVMKeyCode, addrB[:])
	codeStale := keys.BuildEVMKey(keys.EVMKeyCode, addrStale[:])

	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	codeHashVal := make([]byte, vtype.CodeHashLen)
	codeHashVal[31] = 0xAB
	codeVal := []byte{0x60, 0x80}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageA, Value: padLeft32(0x0A)},
			{Key: storageStale, Value: padLeft32(0x0C)},
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

	var found bool
	for _, k := range staleKeys {
		_, found = s.Get(keys.EVMStoreKey, k)
		require.True(t, found, "pre-import: key should exist")
	}

	// --- Phase 2: build a snapshot that only contains addrA/addrB data ---
	src := setupTestStore(t)
	defer src.Close()

	newStorageVal := padLeft32(0xA1)
	newNonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 5}
	newCodeHashVal := make([]byte, vtype.CodeHashLen)
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
	require.NoError(t, imp.AddModule("flatkv"))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	// --- Phase 4: verify stale keys are gone across all DB types ---
	var got []byte
	got, found = s.Get(keys.EVMStoreKey, storageA)
	require.True(t, found, "storage key A should exist")
	require.Equal(t, newStorageVal, got)

	got, found = s.Get(keys.EVMStoreKey, nonceA)
	require.True(t, found, "nonce key A should exist")
	require.Equal(t, newNonceVal, got)

	got, found = s.Get(keys.EVMStoreKey, codeB)
	require.True(t, found, "code key B should exist")
	require.Equal(t, newCodeVal, got)

	got, found = s.Get(keys.EVMStoreKey, codeHashB)
	require.True(t, found, "codehash key B should exist")
	require.Equal(t, newCodeHashVal, got)

	for _, k := range staleKeys {
		_, found = s.Get(keys.EVMStoreKey, k)
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
		_, found = s.Get(keys.EVMStoreKey, k)
		require.False(t, found, "stale key must remain absent after reopen")
	}
	require.Equal(t, srcHash, s.RootHash())
}

func TestImporterFailsWhenResetCannotRemoveCurrentLink(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
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
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01))),
		padLeft32(0x11), false,
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	// Non-leaf nodes (Height != 0) are silently skipped.
	imp.AddNode(&types.SnapshotNode{
		Key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01))),
		Value:  padLeft32(0x11),
		Height: 1, // non-leaf
	})

	require.NoError(t, imp.Close())

	// Data should NOT have been imported.
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01)))
	_, found := s.Get(keys.EVMStoreKey, key)
	require.False(t, found, "height != 0 node should be skipped")
	require.NoError(t, s.Close())
}

func TestImporterNilKeySkipped(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
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
	cfg := config.DefaultTestConfig(t)
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	// A key without module prefix ("/" separator) should be rejected by
	// routePhysicalKey during flush.
	imp.AddNode(&types.SnapshotNode{
		Key:     []byte{0xDE, 0xAD},
		Value:   []byte{0x01, 0x02},
		Version: 1,
	})

	err = imp.Close()
	require.Error(t, err, "import with invalid physical key should fail")
	require.Contains(t, err.Error(), "route key")
}

func TestImporterDoubleImport(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	storageVal1 := padLeft32(0x11)
	sv1 := &[32]byte{}
	copy(sv1[:], storageVal1)
	storageVal2 := padLeft32(0x22)
	sv2 := &[32]byte{}
	copy(sv2[:], storageVal2)

	// First import — uses physical keys and serialized VType values.
	imp1, err := s.Importer(1)
	require.NoError(t, err)
	imp1.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x01), slotN(0x01)),
		Value:   vtype.NewStorageData().SetBlockHeight(1).SetValue(sv1).Serialize(),
		Version: 1,
	})
	require.NoError(t, imp1.Close())

	key1 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01)))
	val, found := s.Get(keys.EVMStoreKey, key1)
	require.True(t, found)
	require.Equal(t, storageVal1, val)

	// Second import: should wipe prior state (resetForImport).
	imp2, err := s.Importer(2)
	require.NoError(t, err)
	imp2.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x02), slotN(0x02)),
		Value:   vtype.NewStorageData().SetBlockHeight(2).SetValue(sv2).Serialize(),
		Version: 2,
	})
	require.NoError(t, imp2.Close())

	require.Equal(t, int64(2), s.Version())

	// Data from first import should be gone.
	_, found = s.Get(keys.EVMStoreKey, key1)
	require.False(t, found, "first import data should be wiped by second import")

	key2 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x02), slotN(0x02)))
	val, found = s.Get(keys.EVMStoreKey, key2)
	require.True(t, found)
	require.Equal(t, storageVal2, val)
	require.NoError(t, s.Close())
}

func TestExporterAtHistoricalVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	// Keep enough historical snapshots that v1 survives after committing v3
	// (latest v3 + the two older snapshots v2 and v1); the default keep-recent
	// of 1 would prune v1 and make the historical export below fail.
	cfg.SnapshotKeepRecent = 2
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x10)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))

	// v1: write 0x11
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// v2: write 0x22
	cs2 := makeChangeSet(key, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	// v3: write 0x33
	cs3 := makeChangeSet(key, padLeft32(0x33), false)
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
		if _, ok := item.(string); ok {
			continue // skip the module header
		}
		node := item.(*types.SnapshotNode)
		kind, _, parseErr := ktype.StripEVMPhysicalKey(node.Key)
		require.NoError(t, parseErr)
		if kind == keys.EVMKeyStorage {
			storageNodes = append(storageNodes, node)
		}
	}
	require.NoError(t, exp.Close())

	require.Len(t, storageNodes, 1)
	sd, err := vtype.DeserializeStorageData(storageNodes[0].Value)
	require.NoError(t, err)
	require.Equal(t, padLeft32(0x11), sd.GetValue()[:], "historical export should have v1 value")
}

func TestExportImportLargerDataset(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	// Write multiple key types across multiple addresses in a single block
	// so that all rows share the same block height. The importer commits
	// everything at a single version, so block heights must match for the
	// LtHash round-trip to be identical.
	var allPairs []*proto.KVPair
	for i := byte(1); i <= 10; i++ {
		addr := addrN(i)
		allPairs = append(allPairs,
			noncePair(addr, uint64(i)),
			&proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(i))),
				Value: padLeft32(i, i, i),
			},
		)
		if i%3 == 0 {
			allPairs = append(allPairs,
				codeHashPair(addr, codeHashN(i)),
				codePair(addr, []byte{0x60, i}),
			)
		}
	}
	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: allPairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)
	originalHash := s.RootHash()

	// Export.
	exp, err := s.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Greater(t, len(nodes), 0)

	// Import into a fresh store.
	dir2 := t.TempDir()
	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = filepath.Join(dir2, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s2.Importer(1)
	require.NoError(t, err)
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	require.Equal(t, int64(1), s2.Version())
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
	require.NoError(t, batch.Set(accountPhysKey(addr), []byte{0xDE, 0xAD}))
	require.NoError(t, batch.Commit(dbtypes.WriteOptions{Sync: true}))
	_ = batch.Close()

	// Raw exporter does not parse values — corrupt data is exported as-is.
	exp := NewKVExporter(s, s.Version())

	var nodes []*types.SnapshotNode
	for {
		item, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone))
			break
		}
		if _, ok := item.(string); ok {
			continue // skip the module header
		}
		node, ok := item.(*types.SnapshotNode)
		require.True(t, ok)
		nodes = append(nodes, node)
	}
	require.Len(t, nodes, 1, "corrupt value should still be exported as raw bytes")
	require.Equal(t, []byte{0xDE, 0xAD}, nodes[0].Value)
	if exp.iter != nil {
		_ = exp.iter.Close()
		exp.iter = nil
	}
}

// TestExporterImporterNonEVMLegacyRoundTrip drives non-EVM module data
// (e.g. "bank", "staking") through the full Export → Import pipeline to
// cover the module-prefixed legacyDB path introduced in PR #3229.
//
// The path exercised here is NOT reachable from a rootmulti-level
// integration test because CompositeCommitStore.ApplyChangeSets filters
// non-EVM changesets out before they ever reach FlatKV — so writing,
// exporting, and re-importing non-EVM data has to be tested at this layer.
//
// Invariants verified:
//  1. ApplyChangeSets routes non-EVM modules to legacyDB under a
//     "<module>/" physical-key prefix via classifyAndPrefix.
//  2. The Exporter emits raw physical keys carrying that prefix.
//  3. The Importer re-routes via routePhysicalKey, sending non-EVM keys
//     back to legacyDB with the prefix intact.
//  4. Get(moduleName, key) reads them back correctly, and cross-module
//     namespaces stay isolated (same inner bytes under a different module
//     must miss).
//  5. Deletions physically remove the key — deleted entries must not
//     resurface in the Exporter output.
//  6. The imported store's LtHash matches the source bit-for-bit, and
//     VerifyLtHash passes on the imported store (full-scan ≡ committed).
func TestExporterImporterNonEVMLegacyRoundTrip(t *testing.T) {
	src := setupTestStore(t)
	defer func() { require.NoError(t, src.Close()) }()

	bankKVs := map[string][]byte{
		"balance/alice": []byte("100"),
		"balance/bob":   []byte("200"),
		"supply":        []byte("300"),
	}
	stakingKVs := map[string][]byte{
		"validator/0": {0x01, 0x02, 0x03},
		"validator/1": {0x04, 0x05, 0x06},
	}
	deletedBankKey := []byte("balance/charlie")

	addr := addrN(0x42)
	slot := slotN(0x01)
	evmStorageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	evmStorageVal := padLeft32(0xAB)

	bankPairs := make([]*proto.KVPair, 0, len(bankKVs)+1)
	for k, v := range bankKVs {
		bankPairs = append(bankPairs, &proto.KVPair{Key: []byte(k), Value: v})
	}
	// Pre-seed then delete to exercise the delete path: the physical key
	// must be absent from the exporter output (batch.Delete at commit
	// time).
	bankPairs = append(bankPairs, &proto.KVPair{Key: deletedBankKey, Value: []byte("doomed")})

	stakingPairs := make([]*proto.KVPair, 0, len(stakingKVs))
	for k, v := range stakingKVs {
		stakingPairs = append(stakingPairs, &proto.KVPair{Key: []byte(k), Value: v})
	}

	// Block 1: mixed changeset — non-EVM and EVM in the same commit — so
	// classifyAndPrefix has to route both kinds in one pass.
	require.NoError(t, src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: bankPairs}},
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: stakingPairs}},
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: evmStorageKey, Value: evmStorageVal},
		}}},
	}))
	commitAndCheck(t, src)

	// Block 2: delete the doomed bank key. MarkDeleted → batch.Delete at
	// commit time, so the physical row must be gone from the exporter.
	require.NoError(t, src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: deletedBankKey, Delete: true},
		}}},
	}))
	commitAndCheck(t, src)

	srcHash := src.RootHash()

	exp, err := src.Exporter(2)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Greater(t, len(nodes), 0)

	// Sanity on the exporter output: at least one "bank/" and one
	// "staking/" node must appear (the whole point of this test), and
	// the deleted bank key must not.
	sawBank := false
	sawStaking := false
	for _, n := range nodes {
		mod, inner, err := ktype.StripModulePrefix(n.Key)
		require.NoErrorf(t, err, "exported key missing module prefix: %x", n.Key)
		switch mod {
		case "bank":
			sawBank = true
			require.NotEqualf(t, string(deletedBankKey), string(inner),
				"deleted bank key must not appear in exporter output")
		case "staking":
			sawStaking = true
		}
	}
	require.True(t, sawBank, "expected at least one bank/ node in export")
	require.True(t, sawStaking, "expected at least one staking/ node in export")

	dst := setupTestStore(t)
	imp, err := dst.Importer(2)
	require.NoError(t, err)
	require.NoError(t, imp.AddModule("flatkv"))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	require.Equal(t, int64(2), dst.Version())

	for k, want := range bankKVs {
		got, found := dst.Get("bank", []byte(k))
		require.Truef(t, found, "bank/%s missing after import", k)
		require.Equalf(t, want, got, "bank/%s value mismatch", k)
	}
	for k, want := range stakingKVs {
		got, found := dst.Get("staking", []byte(k))
		require.Truef(t, found, "staking/%s missing after import", k)
		require.Equalf(t, want, got, "staking/%s value mismatch", k)
	}

	_, found := dst.Get("bank", deletedBankKey)
	require.False(t, found, "deleted bank key should not resurrect on import")

	got, found := dst.Get(keys.EVMStoreKey, evmStorageKey)
	require.True(t, found, "mixed-block EVM key missing after import")
	require.Equal(t, evmStorageVal, got)

	// Cross-module isolation: looking up a bank inner-key under "staking"
	// must miss. Guards against a future refactor accidentally pooling
	// all non-EVM keys into one namespace.
	_, found = dst.Get("staking", []byte("balance/alice"))
	require.False(t, found, "staking module should not see bank keys")

	// Round-trip LtHash invariance: import recomputes the LtHash from the
	// same physical key/value pairs, so the global RootHash must match
	// bit-for-bit.
	require.Equalf(t, srcHash, dst.RootHash(),
		"RootHash after non-EVM round-trip mismatch")

	// Full-scan verification catches any silent drift between legacyDB's
	// physical layout and the per-DB LtHash accumulator on the imported
	// store.
	require.NoError(t, VerifyLtHash(dst),
		"VerifyLtHash should pass on imported store with legacy data")

	require.NoError(t, dst.Close())
}
