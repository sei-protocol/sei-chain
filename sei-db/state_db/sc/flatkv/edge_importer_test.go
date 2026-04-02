package flatkv

import (
	"errors"
	"path/filepath"
	"testing"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

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
	imp.AddNode(&scTypes.SnapshotNode{
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
	imp.AddNode(&scTypes.SnapshotNode{
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
	imp.AddNode(&scTypes.SnapshotNode{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		Value: []byte{0x11},
	})

	// Add a node with a nonce key but invalid nonce value length.
	// This should cause ApplyChangeSets to error during flush/close.
	addr2 := addrN(0x02)
	imp.AddNode(&scTypes.SnapshotNode{
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
	imp1.AddNode(&scTypes.SnapshotNode{
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
	imp2.AddNode(&scTypes.SnapshotNode{
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

	var storageNodes []*scTypes.SnapshotNode
	for {
		item, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone))
			break
		}
		node := item.(*scTypes.SnapshotNode)
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
