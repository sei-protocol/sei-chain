package flatkv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
	"github.com/stretchr/testify/require"
)

// fullScanPerDBLtHash computes LtHash for each data DB individually via full scan.
func fullScanPerDBLtHash(t *testing.T, s *CommitStore) map[string]*lthash.LtHash {
	t.Helper()
	result := make(map[string]*lthash.LtHash, 4)
	for dbDir, db := range map[string]types.KeyValueDB{
		accountDBDir: s.accountDB,
		codeDBDir:    s.codeDB,
		storageDBDir: s.storageDB,
		legacyDBDir:  s.legacyDB,
	} {
		h, err := fullScanDBLtHash(db)
		require.NoError(t, err)
		result[dbDir] = h
	}
	return result
}

// verifyPerDBLtHash checks that the in-memory per-DB working hashes
// match a full scan of each respective database.
func verifyPerDBLtHash(t *testing.T, s *CommitStore) {
	t.Helper()
	scanned := fullScanPerDBLtHash(t, s)
	for dbDir, scanHash := range scanned {
		require.True(t, s.perDBWorkingLtHash[dbDir].Equal(scanHash),
			"per-DB LtHash mismatch for %s:\n  working:  %x\n  fullscan: %x",
			dbDir, s.perDBWorkingLtHash[dbDir].Checksum(), scanHash.Checksum())
	}
}

// commitMixedState applies changesets with data across all 4 DB types.
func commitMixedState(t *testing.T, s *CommitStore, round int) {
	t.Helper()
	addr := addrN(byte(round))
	slot := slotN(byte(round))
	legacyKey := append([]byte{0x09}, addr[:]...)

	cs1 := namedCS(
		noncePair(addr, uint64(round)),
		codeHashPair(addr, codeHashN(byte(round))),
		codePair(addr, []byte{0x60, 0x80, byte(round)}),
		storagePair(addr, slot, []byte{byte(round), 0xAA}),
	)
	cs2 := makeChangeSet(legacyKey, []byte{byte(round), 0xBB}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1, cs2}))
	_, err := s.Commit()
	require.NoError(t, err)
}

// deletePerDBKeysFromMetadataDB simulates a pre-upgrade store by removing
// per-DB LTHash keys from metadataDB.
func deletePerDBKeysFromMetadataDB(t *testing.T, metadataDBPath string) {
	t.Helper()
	db, err := pebbledb.Open(context.Background(), metadataDBPath, types.OpenOptions{}, false)
	require.NoError(t, err)
	for _, metaKey := range perDBLtHashKeys {
		_ = db.Delete([]byte(metaKey), types.WriteOptions{})
	}
	require.NoError(t, db.Close())
}

// simulateUpgrade removes per-DB LTHash keys from both the snapshot and
// working directory's metadataDB, and removes SNAPSHOT_BASE to force re-clone.
func simulateUpgrade(t *testing.T, flatkvDir string) {
	t.Helper()
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	require.NoError(t, err)
	deletePerDBKeysFromMetadataDB(t, filepath.Join(snapDir, metadataDir))

	workingMetaPath := filepath.Join(flatkvDir, workingDirName, metadataDir)
	if _, err := os.Stat(workingMetaPath); err == nil {
		deletePerDBKeysFromMetadataDB(t, workingMetaPath)
	}
	_ = os.Remove(filepath.Join(flatkvDir, workingDirName, snapshotBaseFile))
}

// Test 1: Crash recovery with global/local skew -- verify per-DB LTHash
// is correct after catchup replays the skewed version.
func TestPerDBLtHashSkewRecovery(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	verifyPerDBLtHash(t, s1)
	require.NoError(t, s1.Close())

	// Tamper with accountDB's LocalMeta to simulate incomplete commit
	// (accountDB thinks it's at v1, but global says v2)
	flatkvDir := dbDir
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	require.NoError(t, err)

	accountDBPath := filepath.Join(snapDir, accountDBDir)
	db, err := pebbledb.Open(t.Context(), accountDBPath, types.OpenOptions{}, false)
	require.NoError(t, err)
	lagMeta := &LocalMeta{CommittedVersion: 1}
	require.NoError(t, db.Set(DBLocalMetaKey, MarshalLocalMeta(lagMeta), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	// Reopen -- should detect skew and catchup
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 2)
}

// Test 2: Upgrade from old format -- delete per-DB keys from metadataDB,
// reopen, verify backfill produces correct per-DB hashes AFTER catchup.
func TestPerDBLtHashUpgradeBackfill(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	commitMixedState(t, s1, 3)
	verifyPerDBLtHash(t, s1)
	require.NoError(t, s1.Close())

	// Simulate pre-upgrade format by removing per-DB keys
	simulateUpgrade(t, dbDir)

	// Reopen -- triggers backfill
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version())
	require.False(t, s2.needsPerDBBackfill, "backfill should have cleared the flag")
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 3)
}

// Test 3: Upgrade + skew combined -- old format + tampered LocalMeta version,
// verify backfill after catchup still produces correct per-DB hashes.
func TestPerDBLtHashUpgradeWithSkew(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	require.NoError(t, s1.Close())

	flatkvDir := dbDir

	// Simulate upgrade
	simulateUpgrade(t, flatkvDir)

	// Tamper accountDB in snapshot to simulate crash skew (version 1 instead of 2)
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	require.NoError(t, err)
	accountDBPath := filepath.Join(snapDir, accountDBDir)
	db, err := pebbledb.Open(t.Context(), accountDBPath, types.OpenOptions{}, false)
	require.NoError(t, err)
	lagMeta := &LocalMeta{CommittedVersion: 1}
	require.NoError(t, db.Set(DBLocalMetaKey, MarshalLocalMeta(lagMeta), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	// Reopen -- catchup replays v2 with zero per-DB hashes, then backfill scans
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 2)
}

// Test 4: Crash between catchup metadata flush and backfill -- simulate crash
// after catchup commitGlobalMetadata but before backfill persist, reopen and
// verify per-DB keys are still absent so backfill re-triggers.
func TestPerDBLtHashCrashBetweenCatchupAndBackfill(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	commitMixedState(t, s1, 3)
	require.NoError(t, s1.Close())

	flatkvDir := dbDir
	simulateUpgrade(t, flatkvDir)

	// Open without calling backfill by doing a manual partial open:
	// open() calls loadGlobalMetadata which sets needsPerDBBackfill = true.
	// Then catchup runs, calling commitGlobalMetadata -- it must NOT write per-DB keys.
	// We then close without running backfill to simulate a crash.
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	require.NoError(t, s2.open())
	require.True(t, s2.needsPerDBBackfill, "backfill flag should be set after load")
	require.NoError(t, s2.catchup(0))
	require.NoError(t, s2.Close())

	// Verify per-DB keys are still absent in the working dir's metadataDB.
	// (open() cloned from snapshot which has keys deleted.)
	workingMetaPath := filepath.Join(flatkvDir, workingDirName, metadataDir)
	mdb, err := pebbledb.Open(t.Context(), workingMetaPath, types.OpenOptions{}, false)
	require.NoError(t, err)
	for dbName, metaKey := range perDBLtHashKeys {
		_, getErr := mdb.Get([]byte(metaKey))
		require.Error(t, getErr, "per-DB key %s (%s) should still be absent after crash window", metaKey, dbName)
	}
	require.NoError(t, mdb.Close())

	// Full reopen -- should re-trigger backfill
	_ = os.Remove(filepath.Join(flatkvDir, workingDirName, snapshotBaseFile))
	s3 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s3.LoadVersion(0, false)
	require.NoError(t, err)
	defer s3.Close()

	require.Equal(t, int64(3), s3.Version())
	require.False(t, s3.needsPerDBBackfill)
	verifyPerDBLtHash(t, s3)
	verifyLtHashAtHeight(t, s3, 3)
}

// Test 5: Per-DB full scan verification after restart.
func TestPerDBLtHashPersistenceAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 1; i <= 10; i++ {
		commitMixedState(t, s1, i)
	}
	verifyPerDBLtHash(t, s1)
	require.NoError(t, s1.Close())

	// Reopen and verify
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(10), s2.Version())
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 10)

	// Also verify committed hashes match working hashes (just opened, no pending writes)
	for dbDir, wh := range s2.perDBWorkingLtHash {
		ch := s2.perDBCommittedLtHash[dbDir]
		require.True(t, wh.Equal(ch),
			"per-DB committed and working hashes should match on open for %s", dbDir)
	}
}

// Test 6: ReadOnly open does not persist backfill.
func TestPerDBLtHashReadOnlyNoPersist(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	require.NoError(t, s1.Close())

	simulateUpgrade(t, dbDir)

	// Open as readonly -- backfill runs in memory
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	roStore, err := s2.LoadVersion(0, true)
	require.NoError(t, err)
	roCS := roStore.(*CommitStore)
	verifyPerDBLtHash(t, roCS)
	require.NoError(t, roStore.Close())

	// Open as read-write -- should still need backfill since readonly didn't persist
	s3 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s3.LoadVersion(0, false)
	require.NoError(t, err)
	defer s3.Close()

	require.False(t, s3.needsPerDBBackfill, "backfill should have run and persisted on RW open")
	verifyPerDBLtHash(t, s3)
	verifyLtHashAtHeight(t, s3, 2)
}

// Test 7: Verify per-DB LTHash alongside global in the incremental 100-block test.
func TestPerDBLtHashIncrementalEqualsFullScan(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Blocks 1-10: accounts + storage + code + legacy
	for i := 1; i <= 10; i++ {
		addr := addrN(byte(i))
		slot := slotN(byte(i))
		legacyKey := append([]byte{0x09}, addr[:]...)

		cs1 := namedCS(
			noncePair(addr, uint64(i)),
			storagePair(addr, slot, []byte{byte(i), 0xAA}),
		)
		cs2 := makeChangeSet(legacyKey, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1, cs2}))
		commitAndCheck(t, s)
	}
	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 10)

	// Blocks 11-15: deploy code
	for i := 11; i <= 15; i++ {
		addr := addrN(byte(i - 10))
		ch := codeHashN(byte(i))
		cs := namedCS(
			codeHashPair(addr, ch),
			codePair(addr, []byte{0x60, 0x80, byte(i)}),
		)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyPerDBLtHash(t, s)

	// Blocks 16-20: update storage + delete some storage
	for i := 16; i <= 18; i++ {
		addr := addrN(byte(i - 15))
		slot := slotN(byte(i - 15))
		cs := namedCS(storagePair(addr, slot, []byte{byte(i), 0xBB}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	for i := 19; i <= 20; i++ {
		addr := addrN(byte(i - 15))
		slot := slotN(byte(i - 15))
		cs := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 20)
}

// Test: sum of per-DB hashes equals global hash (homomorphic property).
func TestPerDBLtHashSumEqualsGlobal(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	for i := 1; i <= 5; i++ {
		commitMixedState(t, s, i)
	}

	// Compute the "sum" of all per-DB hashes
	sumHash := lthash.New()
	for _, dbDir := range []string{accountDBDir, codeDBDir, storageDBDir, legacyDBDir} {
		sumHash.MixIn(s.perDBWorkingLtHash[dbDir])
	}

	require.True(t, s.workingLtHash.Equal(sumHash),
		"sum of per-DB LtHashes should equal global LtHash:\n  global: %x\n  sum:    %x",
		s.workingLtHash.Checksum(), sumHash.Checksum())
}

// Test: per-DB hashes are correct after catchup with WAL replay.
func TestPerDBLtHashCatchupReplay(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	require.NoError(t, s1.WriteSnapshot(""))

	commitMixedState(t, s1, 3)
	commitMixedState(t, s1, 4)
	commitMixedState(t, s1, 5)
	verifyPerDBLtHash(t, s1)

	// Save expected hashes for comparison
	expectedPerDB := make(map[string][32]byte, 4)
	for dbDir, h := range s1.perDBWorkingLtHash {
		expectedPerDB[dbDir] = h.Checksum()
	}
	require.NoError(t, s1.Close())

	// Reopen: catchup from v2 snapshot through v3,v4,v5 via WAL
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	for dbDir, expectedCS := range expectedPerDB {
		actualCS := s2.perDBWorkingLtHash[dbDir].Checksum()
		require.Equal(t, expectedCS, actualCS,
			"per-DB LtHash mismatch for %s after catchup", dbDir)
	}
	verifyPerDBLtHash(t, s2)
}

// Test: per-DB LtHash with empty blocks doesn't drift.
func TestPerDBLtHashEmptyBlocks(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	commitMixedState(t, s, 1)
	checksums := make(map[string][32]byte)
	for dbDir, h := range s.perDBWorkingLtHash {
		checksums[dbDir] = h.Checksum()
	}

	// 5 empty blocks
	for i := 0; i < 5; i++ {
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS()}))
		commitAndCheck(t, s)
	}

	for dbDir, expected := range checksums {
		actual := s.perDBWorkingLtHash[dbDir].Checksum()
		require.Equal(t, expected, actual,
			"empty blocks should not change per-DB LtHash for %s", dbDir)
	}
}

// Test: per-DB hashes after import via Importer.
func TestPerDBLtHashAfterImport(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := s.Importer(1)
	require.NoError(t, err)

	for i := byte(1); i <= 5; i++ {
		addr := addrN(i)
		slot := slotN(i)
		imp.AddNode(&scTypes.SnapshotNode{Key: storagePair(addr, slot, []byte{i, 0xAA}).Key, Value: storagePair(addr, slot, []byte{i, 0xAA}).Value, Height: 0})
		imp.AddNode(&scTypes.SnapshotNode{Key: noncePair(addr, uint64(i)).Key, Value: noncePair(addr, uint64(i)).Value, Height: 0})
	}
	require.NoError(t, imp.Close())

	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 1)

	for dbDir, wh := range s.perDBWorkingLtHash {
		ch := s.perDBCommittedLtHash[dbDir]
		require.True(t, wh.Equal(ch),
			"per-DB committed and working hashes should match after import for %s", dbDir)
	}
	require.NoError(t, s.Close())
}

// Test: per-DB hashes survive rollback.
func TestPerDBLtHashRollback(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s, 1)
	commitMixedState(t, s, 2)
	commitMixedState(t, s, 3)
	require.NoError(t, s.WriteSnapshot(""))

	commitMixedState(t, s, 4)
	commitMixedState(t, s, 5)

	// Rollback to v3
	require.NoError(t, s.Rollback(3))
	require.Equal(t, int64(3), s.Version())
	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 3)

	require.NoError(t, s.Close())
}

// Test: rollback on a store that was upgraded (missing per-DB keys in metadataDB).
// This exercises the backfill path inside Rollback.
func TestPerDBLtHashRollbackAfterUpgrade(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s, 1)
	commitMixedState(t, s, 2)
	commitMixedState(t, s, 3)
	require.NoError(t, s.WriteSnapshot(""))

	commitMixedState(t, s, 4)
	commitMixedState(t, s, 5)
	require.NoError(t, s.Close())

	simulateUpgrade(t, dbDir)

	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s2.Rollback(3))
	require.Equal(t, int64(3), s2.Version())
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 3)

	require.NoError(t, s2.Close())
}

// Test: backfill persists correct LtHash to each DB's LocalMeta.
func TestPerDBLtHashBackfillUpdatesLocalMeta(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	require.NoError(t, s1.Close())

	simulateUpgrade(t, dbDir)

	// Reopen -- triggers backfill which should also update LocalMeta
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	// Verify LocalMeta has LtHash for each DB
	for dbDir, meta := range s2.localMeta {
		require.NotNil(t, meta.LtHash,
			"LocalMeta.LtHash should be set for %s after backfill", dbDir)
		expected := s2.perDBWorkingLtHash[dbDir]
		require.True(t, meta.LtHash.Equal(expected),
			"LocalMeta.LtHash should match working hash for %s:\n  meta:    %x\n  working: %x",
			dbDir, meta.LtHash.Checksum(), expected.Checksum())
	}
}

// Test: per-DB keys are present in metadataDB after normal commit cycle.
func TestPerDBLtHashPersistedInMetadataDB(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s, 1)
	commitMixedState(t, s, 2)

	// Read per-DB keys directly from metadataDB
	for dbDir, metaKey := range perDBLtHashKeys {
		data, err := s.metadataDB.Get([]byte(metaKey))
		require.NoError(t, err, "per-DB key %s should exist in metadataDB after commit", dbDir)
		h, err := lthash.Unmarshal(data)
		require.NoError(t, err)
		require.True(t, s.perDBCommittedLtHash[dbDir].Equal(h),
			"metadataDB per-DB hash should match committed hash for %s", dbDir)
	}

	require.NoError(t, s.Close())
}

func TestPerDBLtHashAfterDirectImport(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s.LoadVersion(0, false)
	require.NoError(t, err)

	var pairs []*iavl.KVPair
	for i := byte(1); i <= 10; i++ {
		addr := addrN(i)
		slot := slotN(i)
		pairs = append(pairs,
			storagePair(addr, slot, []byte{i, 0xAA}),
			noncePair(addr, uint64(i)),
		)
	}

	cs := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 1)
	require.NoError(t, s.Close())
}
