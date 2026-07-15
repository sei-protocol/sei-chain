package flatkv

import (
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

// fullScanModuleStats tallies, per module, the live key count and total
// footprint (physical key bytes + stored value bytes) of every non-meta key in
// db. This is the ground truth the incrementally-maintained working stats must
// always equal. Test-only.
func fullScanModuleStats(t *testing.T, db types.KeyValueDB) map[string]lthash.ModuleStats {
	t.Helper()
	iter, err := db.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	defer iter.Close()

	out := make(map[string]lthash.ModuleStats)
	for ; iter.Valid(); iter.Next() {
		if ktype.IsMetaKey(iter.Key()) {
			continue
		}
		module, _, err := ktype.StripModulePrefix(iter.Key())
		require.NoError(t, err)
		st := out[module]
		st.KeyCount++
		st.Bytes += int64(len(iter.Key())) + int64(len(iter.Value()))
		out[module] = st
	}
	require.NoError(t, iter.Error())
	return out
}

// verifyModuleStats asserts the in-memory per-module working stats exactly
// match a full scan of each data DB. Modules whose keys were all deleted keep a
// zeroed working entry (mirroring the per-module hash) and must not appear with
// non-zero counts.
func verifyModuleStats(t *testing.T, s *CommitStore) {
	t.Helper()
	for _, ndb := range s.namedDataDBs() {
		scanned := fullScanModuleStats(t, ndb.db)
		working := s.perDBModuleWorkingStats[ndb.dir]

		for module, want := range scanned {
			require.Equal(t, want, working[module],
				"per-module stats mismatch for %s/%s", ndb.dir, module)
		}
		for module, got := range working {
			if _, ok := scanned[module]; !ok {
				require.Equal(t, lthash.ModuleStats{}, got,
					"stale non-zero working stats for emptied module %s/%s", ndb.dir, module)
			}
		}
	}
}

// Test: per-module stats are maintained incrementally and always equal a full
// scan across a sequence of multi-module blocks.
func TestPerModuleStatsIncrementalEqualsFullScan(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	for i := byte(1); i <= 5; i++ {
		commitMultiModuleState(t, s, i)
		verifyModuleStats(t, s)
	}

	// Sanity: miscDB tracks evm + gov + bank, each with the expected key count.
	misc := s.perDBModuleWorkingStats[miscDBDir]
	require.Equal(t, int64(5), misc[keys.EVMStoreKey].KeyCount, "one evm-misc key per round")
	require.Equal(t, int64(10), misc["gov"].KeyCount, "two gov keys per round")
	require.Equal(t, int64(5), misc["bank"].KeyCount, "one bank key per round")
}

// Test: add -> update -> delete transitions move KeyCount and Bytes exactly as
// specified (add: +1 key + full footprint; update: same count, byte delta only;
// delete: -1 key back to zero).
func TestPerModuleStatsAddUpdateDeleteTransitions(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	govKey := []byte{0x01, 0x2A}
	physKeyLen := int64(len(ktype.ModulePhysicalKey("gov", govKey)))
	stats := func() lthash.ModuleStats { return s.perDBModuleWorkingStats[miscDBDir]["gov"] }

	// Add: one key with a short value. Footprint must exceed the physical key
	// length (key bytes are always counted, plus a non-empty serialized value).
	shortVal := []byte{0xAA}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		moduleCS("gov", &proto.KVPair{Key: govKey, Value: shortVal}),
	}))
	commitAndCheck(t, s)
	verifyModuleStats(t, s)
	afterAdd := stats()
	require.Equal(t, int64(1), afterAdd.KeyCount)
	storedShort := afterAdd.Bytes
	require.Greater(t, storedShort, physKeyLen, "footprint must include key + non-empty value bytes")

	// Update to a longer value: count unchanged, bytes grow by the value delta.
	longVal := []byte{0xBB, 0xCC, 0xDD, 0xEE}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		moduleCS("gov", &proto.KVPair{Key: govKey, Value: longVal}),
	}))
	commitAndCheck(t, s)
	verifyModuleStats(t, s)
	afterUpdate := stats()
	require.Equal(t, int64(1), afterUpdate.KeyCount, "update must not change key count")
	require.Greater(t, afterUpdate.Bytes, storedShort, "longer value should grow the footprint")

	// Delete: back to an empty module (zeroed working entry).
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		moduleCS("gov", &proto.KVPair{Key: govKey, Delete: true}),
	}))
	commitAndCheck(t, s)
	verifyModuleStats(t, s)
	require.Equal(t, lthash.ModuleStats{}, stats(), "delete of the last key returns stats to zero")
}

// Test: per-module stats persist in each DB's LocalMeta and reload after reopen.
func TestPerModuleStatsPersistenceAfterReopen(t *testing.T) {
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
	verifyModuleStats(t, s1)

	expected := make(map[string]map[string]lthash.ModuleStats)
	for dir, mods := range s1.perDBModuleWorkingStats {
		expected[dir] = make(map[string]lthash.ModuleStats)
		for module, st := range mods {
			expected[dir][module] = st
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
	verifyModuleStats(t, s2)

	for dir, mods := range expected {
		for module, want := range mods {
			require.Equal(t, want, s2.perDBModuleWorkingStats[dir][module],
				"working stats mismatch after reopen for %s/%s", dir, module)
		}
	}

	// On-disk LocalMeta carries the same stats.
	dbInstances := map[string]types.KeyValueDB{
		accountDBDir: s2.accountDB,
		codeDBDir:    s2.codeDB,
		storageDBDir: s2.storageDB,
		miscDBDir:    s2.miscDB,
	}
	for dir, db := range dbInstances {
		meta, err := loadLocalMeta(db)
		require.NoError(t, err)
		for module, want := range expected[dir] {
			require.Equal(t, want, meta.ModuleStats[module],
				"persisted stats mismatch for %s/%s", dir, module)
		}
	}
}

// Test: the state-sync importer produces the same per-module stats the live
// commit path would, and they survive a restart.
func TestPerModuleStatsAfterImportSurvivesRestart(t *testing.T) {
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
		imp.AddNode(&scTypes.SnapshotNode{Key: ktype.ModulePhysicalKey("gov", []byte{i}), Value: govVal, Version: 7})
	}
	require.NoError(t, imp.Close())
	verifyModuleStats(t, s1)

	require.Equal(t, int64(5), s1.perDBModuleWorkingStats[storageDBDir][keys.EVMStoreKey].KeyCount)
	require.Equal(t, int64(5), s1.perDBModuleWorkingStats[accountDBDir][keys.EVMStoreKey].KeyCount)
	require.Equal(t, int64(5), s1.perDBModuleWorkingStats[miscDBDir]["gov"].KeyCount)

	expected := make(map[string]map[string]lthash.ModuleStats)
	for dir, mods := range s1.perDBModuleWorkingStats {
		expected[dir] = make(map[string]lthash.ModuleStats)
		for module, st := range mods {
			expected[dir][module] = st
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

	require.Equal(t, int64(7), s2.Version())
	verifyModuleStats(t, s2)
	for dir, mods := range expected {
		for module, want := range mods {
			require.Equal(t, want, s2.perDBModuleWorkingStats[dir][module],
				"stats mismatch after restart for %s/%s", dir, module)
		}
	}
}
