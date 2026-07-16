package flatkv

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// moduleCS builds a changeset for an arbitrary (non-EVM) cosmos module. Its
// keys are routed to miscDB under a "<module>/" physical prefix.
func moduleCS(module string, pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      module,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
}

// fullScanModuleLtHash computes, per module, the LtHash of every non-meta key
// in db by bucketing keys on their "<module>/" physical prefix. Test-only.
func fullScanModuleLtHash(t *testing.T, db types.KeyValueDB) map[string]*lthash.LtHash {
	t.Helper()
	iter, err := db.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	defer iter.Close()

	byModule := make(map[string][]lthash.KVPairWithLastValue)
	for ; iter.Valid(); iter.Next() {
		if ktype.IsMetaKey(iter.Key()) {
			continue
		}
		module, _, err := ktype.StripModulePrefix(iter.Key())
		require.NoError(t, err)
		byModule[module] = append(byModule[module], lthash.KVPairWithLastValue{
			Key:   bytes.Clone(iter.Key()),
			Value: bytes.Clone(iter.Value()),
		})
	}
	require.NoError(t, iter.Error())

	out := make(map[string]*lthash.LtHash, len(byModule))
	for module, pairs := range byModule {
		h, _ := lthash.ComputeLtHash(nil, pairs)
		if h == nil {
			h = lthash.New()
		}
		out[module] = h
	}
	return out
}

// verifyModuleLtHash checks that the in-memory per-module working hashes match
// a full scan of each data DB, and that the per-module hashes homomorphically
// sum to the per-DB root.
func verifyModuleLtHash(t *testing.T, s *CommitStore) {
	t.Helper()
	for _, ndb := range s.namedDataDBs() {
		scanned := fullScanModuleLtHash(t, ndb.db)
		working := s.perDBModuleWorkingLtHash[ndb.dir]

		// Every scanned module must have a matching working hash.
		for module, scanHash := range scanned {
			wh := working[module]
			require.NotNil(t, wh, "missing working per-module hash for %s/%s", ndb.dir, module)
			require.True(t, wh.Equal(scanHash),
				"per-module LtHash mismatch for %s/%s:\n  working:  %x\n  fullscan: %x",
				ndb.dir, module, wh.Checksum(), scanHash.Checksum())
		}

		// The sum of the (non-zero) working per-module hashes must equal the
		// per-DB root hash.
		sum := lthash.New()
		for _, wh := range working {
			sum.MixIn(wh)
		}
		require.True(t, s.perDBWorkingLtHash[ndb.dir].Equal(sum),
			"sum of per-module hashes should equal per-DB root for %s:\n  root: %x\n  sum:  %x",
			ndb.dir, s.perDBWorkingLtHash[ndb.dir].Checksum(), sum.Checksum())
	}
}

// commitMultiModuleState commits a block touching EVM (account/code/storage +
// an EVM misc key) plus two non-EVM cosmos modules ("gov", "bank"), all in one
// block, so miscDB ends up holding several modules.
func commitMultiModuleState(t *testing.T, s *CommitStore, round byte) {
	t.Helper()
	addr := addrN(round)
	slot := slotN(round)
	evmMiscKey := append([]byte{0x09}, addr[:]...)

	evmCS := namedCS(
		noncePair(addr, uint64(round)),
		codeHashPair(addr, codeHashN(round)),
		codePair(addr, []byte{0x60, 0x80, round}),
		storagePair(addr, slot, []byte{round, 0xAA}),
	)
	evmMiscCS := makeChangeSet(evmMiscKey, []byte{round, 0xBB}, false)
	govCS := moduleCS("gov",
		&proto.KVPair{Key: []byte{0x01, round}, Value: []byte{round, 0xC1}},
		&proto.KVPair{Key: []byte{0x02, round}, Value: []byte{round, 0xC2}},
	)
	bankCS := moduleCS("bank",
		&proto.KVPair{Key: []byte{0x01, round}, Value: []byte{round, 0xD1}},
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{evmCS, evmMiscCS, govCS, bankCS}))
	_, err := s.Commit()
	require.NoError(t, err)
}

// Test: per-module hashes are computed incrementally and match a full scan,
// and homomorphically sum to each per-DB root.
func TestPerModuleLtHashIncrementalEqualsFullScan(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	for i := byte(1); i <= 5; i++ {
		commitMultiModuleState(t, s, i)
	}
	verifyModuleLtHash(t, s)

	// miscDB should now carry three modules: evm, gov, bank.
	misc := s.perDBModuleWorkingLtHash[miscDBDir]
	require.Contains(t, misc, keys.EVMStoreKey)
	require.Contains(t, misc, "gov")
	require.Contains(t, misc, "bank")

	// account/code/storage only ever carry the evm module, and that module's
	// hash equals the per-DB root.
	for _, dir := range []string{accountDBDir, codeDBDir, storageDBDir} {
		mod := s.perDBModuleWorkingLtHash[dir]
		require.Len(t, mod, 1, "%s should only track the evm module", dir)
		require.Contains(t, mod, keys.EVMStoreKey)
		require.True(t, mod[keys.EVMStoreKey].Equal(s.perDBWorkingLtHash[dir]),
			"%s evm module hash should equal per-DB root", dir)
	}
}

// Test: per-module hashes are persisted in each DB's LocalMeta and reload
// correctly after reopen.
func TestPerModuleLtHashPersistenceAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 5; i++ {
		commitMultiModuleState(t, s1, i)
	}
	verifyModuleLtHash(t, s1)

	expected := make(map[string]map[string][32]byte)
	for dir, mods := range s1.perDBModuleWorkingLtHash {
		expected[dir] = make(map[string][32]byte)
		for module, h := range mods {
			expected[dir][module] = h.Checksum()
		}
	}
	require.NoError(t, s1.Close())

	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbDir
	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	verifyModuleLtHash(t, s2)

	// Working per-module hashes rehydrated from disk must match pre-close.
	for dir, mods := range expected {
		for module, cs := range mods {
			got := s2.perDBModuleWorkingLtHash[dir][module]
			require.NotNil(t, got, "module %s/%s missing after reopen", dir, module)
			require.Equal(t, cs, got.Checksum(),
				"per-module hash mismatch after reopen for %s/%s", dir, module)
		}
	}

	// LocalMeta persisted the same set on disk.
	dbInstances := map[string]types.KeyValueDB{
		accountDBDir: s2.accountDB,
		codeDBDir:    s2.codeDB,
		storageDBDir: s2.storageDB,
		miscDBDir:    s2.miscDB,
	}
	for dir, db := range dbInstances {
		meta, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, len(expected[dir]), len(meta.ModuleLtHashes),
			"module hash count mismatch on disk for %s", dir)
		for module, cs := range expected[dir] {
			h := meta.ModuleLtHashes[module]
			require.NotNil(t, h, "module %s/%s not persisted", dir, module)
			require.Equal(t, cs, h.Checksum(), "persisted module hash mismatch for %s/%s", dir, module)
		}
	}
}

// Test: deleting every key of a module returns that module's hash to zero
// (and drops out of the full scan while the working map keeps a zero hash).
func TestPerModuleLtHashDeleteModuleZerosHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	govKey := []byte{0x01, 0x01}
	set := moduleCS("gov", &proto.KVPair{Key: govKey, Value: []byte{0xAB}})
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{set}))
	commitAndCheck(t, s)

	zero := lthash.New().Checksum()
	require.NotEqual(t, zero, s.perDBModuleWorkingLtHash[miscDBDir]["gov"].Checksum(),
		"gov module hash should be non-zero after write")

	del := moduleCS("gov", &proto.KVPair{Key: govKey, Delete: true})
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{del}))
	commitAndCheck(t, s)

	require.Equal(t, zero, s.perDBModuleWorkingLtHash[miscDBDir]["gov"].Checksum(),
		"gov module hash should be zero after deleting all its keys")
	verifyModuleLtHash(t, s)
}

// Test: per-module hashes are populated by the Importer path for a fresh store.
func TestPerModuleLtHashAfterImport(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	for i := byte(1); i <= 5; i++ {
		addr := addrN(i)
		slot := slotN(i)
		storVal := vtype.NewStorageData().SetBlockHeight(1).SetValue(&[32]byte{i, 0xAA}).Serialize()
		acctVal := vtype.NewAccountData().SetBlockHeight(1).SetNonce(uint64(i)).Serialize()
		imp.AddNode(&scTypes.SnapshotNode{Key: storagePhysKey(addr, slot), Value: storVal, Version: 1})
		imp.AddNode(&scTypes.SnapshotNode{Key: accountPhysKey(addr), Value: acctVal, Version: 1})

		// A non-EVM module row goes to miscDB under "gov/".
		govVal := vtype.NewMiscData().SetBlockHeight(1).SetValue([]byte{i, 0xC0}).Serialize()
		imp.AddNode(&scTypes.SnapshotNode{Key: ktype.ModulePhysicalKey("gov", []byte{i}), Value: govVal, Version: 1})
	}
	require.NoError(t, imp.Close())

	verifyModuleLtHash(t, s)

	require.Contains(t, s.perDBModuleWorkingLtHash[miscDBDir], "gov")
	require.Contains(t, s.perDBModuleWorkingLtHash[accountDBDir], keys.EVMStoreKey)
	require.Contains(t, s.perDBModuleWorkingLtHash[storageDBDir], keys.EVMStoreKey)
	require.NoError(t, s.Close())
}

// Test: per-module hashes written by a state-sync import survive a process
// restart. This is the durability guarantee for the state-sync path: the
// importer's FinalizeImport persists per-module hashes into each DB's
// LocalMeta and the post-import checkpoint snapshot carries them across
// reopen, so a restarted node rehydrates the same per-module metadata.
func TestPerModuleLtHashStateSyncImportSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s1.Importer(7)
	require.NoError(t, err)

	for i := byte(1); i <= 5; i++ {
		addr := addrN(i)
		slot := slotN(i)
		storVal := vtype.NewStorageData().SetBlockHeight(7).SetValue(&[32]byte{i, 0xAA}).Serialize()
		acctVal := vtype.NewAccountData().SetBlockHeight(7).SetNonce(uint64(i)).Serialize()
		imp.AddNode(&scTypes.SnapshotNode{Key: storagePhysKey(addr, slot), Value: storVal, Version: 7})
		imp.AddNode(&scTypes.SnapshotNode{Key: accountPhysKey(addr), Value: acctVal, Version: 7})

		govVal := vtype.NewMiscData().SetBlockHeight(7).SetValue([]byte{i, 0xC0}).Serialize()
		bankVal := vtype.NewMiscData().SetBlockHeight(7).SetValue([]byte{i, 0xD0}).Serialize()
		imp.AddNode(&scTypes.SnapshotNode{Key: ktype.ModulePhysicalKey("gov", []byte{i}), Value: govVal, Version: 7})
		imp.AddNode(&scTypes.SnapshotNode{Key: ktype.ModulePhysicalKey("bank", []byte{i}), Value: bankVal, Version: 7})
	}
	require.NoError(t, imp.Close())
	verifyModuleLtHash(t, s1)

	expected := make(map[string]map[string][32]byte)
	for dir, mods := range s1.perDBModuleWorkingLtHash {
		expected[dir] = make(map[string][32]byte)
		for module, h := range mods {
			expected[dir][module] = h.Checksum()
		}
	}
	require.NoError(t, s1.Close())

	// Reopen as a fresh store: state must rehydrate purely from the on-disk
	// snapshot written by the import.
	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbDir
	s2, err := NewCommitStore(t.Context(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(7), s2.Version())
	verifyModuleLtHash(t, s2)

	for dir, mods := range expected {
		require.Equal(t, len(mods), len(s2.perDBModuleWorkingLtHash[dir]),
			"module count mismatch after restart for %s", dir)
		for module, cs := range mods {
			got := s2.perDBModuleWorkingLtHash[dir][module]
			require.NotNil(t, got, "module %s/%s missing after restart", dir, module)
			require.Equal(t, cs, got.Checksum(),
				"per-module hash mismatch after restart for %s/%s", dir, module)
		}
	}

	// miscDB must have persisted both cosmos modules across the restart.
	require.Contains(t, s2.perDBModuleWorkingLtHash[miscDBDir], "gov")
	require.Contains(t, s2.perDBModuleWorkingLtHash[miscDBDir], "bank")
	// account/storage only ever carry the evm module.
	require.Contains(t, s2.perDBModuleWorkingLtHash[accountDBDir], keys.EVMStoreKey)
	require.Contains(t, s2.perDBModuleWorkingLtHash[storageDBDir], keys.EVMStoreKey)
}
