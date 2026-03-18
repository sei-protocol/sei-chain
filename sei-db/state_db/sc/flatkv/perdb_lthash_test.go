package flatkv

import (
	"bytes"
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

// testFullScanDBLtHash computes the LtHash of a single data DB by iterating
// all KV pairs (excluding _meta/ metadata keys). Test-only helper.
func testFullScanDBLtHash(t *testing.T, db types.KeyValueDB) *lthash.LtHash {
	t.Helper()
	iter, err := db.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	defer iter.Close()

	var pairs []lthash.KVPairWithLastValue
	for iter.First(); iter.Valid(); iter.Next() {
		if isMetaKey(iter.Key()) {
			continue
		}
		pairs = append(pairs, lthash.KVPairWithLastValue{
			Key:   bytes.Clone(iter.Key()),
			Value: bytes.Clone(iter.Value()),
		})
	}
	require.NoError(t, iter.Error())
	result, _ := lthash.ComputeLtHash(nil, pairs)
	if result == nil {
		return lthash.New()
	}
	return result
}

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
		result[dbDir] = testFullScanDBLtHash(t, db)
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
// round must be in [0, 255] since it is used as a byte to derive unique addresses/slots.
func commitMixedState(t *testing.T, s *CommitStore, round byte) {
	t.Helper()
	addr := addrN(round)
	slot := slotN(round)
	legacyKey := append([]byte{0x09}, addr[:]...)

	cs1 := namedCS(
		noncePair(addr, uint64(round)),
		codeHashPair(addr, codeHashN(round)),
		codePair(addr, []byte{0x60, 0x80, round}),
		storagePair(addr, slot, []byte{round, 0xAA}),
	)
	cs2 := makeChangeSet(legacyKey, []byte{round, 0xBB}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1, cs2}))
	_, err := s.Commit()
	require.NoError(t, err)
}

// Test: Crash recovery where metadataDB is behind data DBs.
// Simulates a crash after commitBatches (step 2) but before
// commitGlobalMetadata (step 4) by rolling back metadataDB's
// global version. Data DBs and their LocalMeta remain at v2.
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

	// Roll back metadataDB global version to 1 to simulate crash
	// after commitBatches completed but before commitGlobalMetadata.
	snapDir, _, err := currentSnapshotDir(dbDir)
	require.NoError(t, err)

	metaDBPath := filepath.Join(snapDir, metadataDir)
	db, err := pebbledb.Open(t.Context(), metaDBPath, types.OpenOptions{}, false)
	require.NoError(t, err)
	require.NoError(t, db.Set(metaVersionKey, versionToBytes(1), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	// Reopen -- catchup should replay version 2 from WAL
	s2 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 2)
}

// Test: Per-DB full scan verification after restart.
func TestPerDBLtHashPersistenceAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s1 := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 10; i++ {
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

	for _, dbDir := range dataDBDirs {
		wh := s2.perDBWorkingLtHash[dbDir]
		meta := s2.localMeta[dbDir]
		require.NotNil(t, meta.LtHash,
			"LocalMeta LtHash should be loaded for %s", dbDir)
		require.True(t, wh.Equal(meta.LtHash),
			"per-DB working hash should match LocalMeta LtHash on open for %s", dbDir)
	}
}

// Test: Verify per-DB LTHash alongside global in the incremental multi-block test.
func TestPerDBLtHashIncrementalEqualsFullScan(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

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

	for i := byte(1); i <= 5; i++ {
		commitMixedState(t, s, i)
	}

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

	expectedPerDB := make(map[string][32]byte, 4)
	for dbDir, h := range s1.perDBWorkingLtHash {
		expectedPerDB[dbDir] = h.Checksum()
	}
	require.NoError(t, s1.Close())

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
		sp := storagePair(addr, slot, []byte{i, 0xAA})
		np := noncePair(addr, uint64(i))
		imp.AddNode(&scTypes.SnapshotNode{Key: sp.Key, Value: sp.Value})
		imp.AddNode(&scTypes.SnapshotNode{Key: np.Key, Value: np.Value})
	}
	require.NoError(t, imp.Close())

	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 1)

	for _, dbDir := range dataDBDirs {
		wh := s.perDBWorkingLtHash[dbDir]
		meta := s.localMeta[dbDir]
		require.NotNil(t, meta.LtHash,
			"LocalMeta LtHash should exist after import for %s", dbDir)
		require.True(t, wh.Equal(meta.LtHash),
			"per-DB working hash should match LocalMeta LtHash after import for %s", dbDir)
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

	require.NoError(t, s.Rollback(3))
	require.Equal(t, int64(3), s.Version())
	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 3)

	require.NoError(t, s.Close())
}

// Test: per-DB LtHashes are persisted in each DB's LocalMeta after normal commit cycle.
func TestPerDBLtHashPersistedInLocalMeta(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	s := NewCommitStore(t.Context(), dbDir, DefaultConfig())
	_, err := s.LoadVersion(0, false)
	require.NoError(t, err)

	commitMixedState(t, s, 1)
	commitMixedState(t, s, 2)

	dbInstances := map[string]types.KeyValueDB{
		accountDBDir: s.accountDB,
		codeDBDir:    s.codeDB,
		storageDBDir: s.storageDB,
		legacyDBDir:  s.legacyDB,
	}
	for _, dbDirName := range dataDBDirs {
		db := dbInstances[dbDirName]
		meta, err := loadLocalMeta(db)
		require.NoError(t, err, "LocalMeta should be readable for %s", dbDirName)
		require.NotNil(t, meta.LtHash,
			"LocalMeta LtHash should be non-nil for %s", dbDirName)
		require.True(t, s.perDBWorkingLtHash[dbDirName].Equal(meta.LtHash),
			"LocalMeta LtHash should match working hash for %s", dbDirName)
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
