package flatkv

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// testFullScanDBLtHash computes the LtHash of a single data DB by iterating
// all KV pairs (excluding _meta/ metadata keys). Test-only helper.
func testFullScanDBLtHash(t *testing.T, db types.KeyValueDB) *lthash.LtHash {
	t.Helper()
	iter, err := db.NewIter(&types.IterOptions{})
	require.NoError(t, err)
	defer iter.Close()

	var pairs []lthash.KVPairWithLastValue
	for ; iter.Valid(); iter.Next() {
		if ktype.IsMetaKey(iter.Key()) {
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
//
// The scan reads the databases directly, which is what makes it independent of the maintained hashes —
// so it first waits for the committed block to reach disk. The stores flush asynchronously, so
// without that wait the scan measures a stale disk and every comparison against it is meaningless.
func fullScanPerDBLtHash(t *testing.T, s *CommitStore) map[string]*lthash.LtHash {
	t.Helper()
	requireFlushedToDisk(t, s)
	result := make(map[string]*lthash.LtHash, 4)
	for _, dir := range dataDBDirs {
		db := s.rawDBFor(dir)
		result[dir] = testFullScanDBLtHash(t, db)
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
	miscKey := append([]byte{0x09}, addr[:]...)

	cs1 := namedCS(
		noncePair(addr, uint64(round)),
		codeHashPair(addr, codeHashN(round)),
		codePair(addr, []byte{0x60, 0x80, round}),
		storagePair(addr, slot, []byte{round, 0xAA}),
	)
	cs2 := makeChangeSet(miscKey, []byte{round, 0xBB}, false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1, cs2}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
}

// Test: crash recovery where one data DB's version record is behind the others.
//
// A data DB commits its version in the same batch as its data, so a torn commit
// leaves the DBs at different versions. The store then opens at the lowest and
// replays from there — into DBs that already hold those blocks. This pins that
// the replay is idempotent: re-applying a block to a DB that already has it is a
// no-op for the LtHash, because the old value read back is the new value.
func TestPerDBLtHashSkewRecovery(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	verifyPerDBLtHash(t, s1)
	wantRoot := bytes.Clone(rootHash(s1))
	wantPerDB := make(map[string][32]byte, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		wantPerDB[dbDir] = s1.perDBWorkingLtHash[dbDir].Checksum()
	}
	// Rewind accountDB's version record to 1, leaving its data — and every other DB — at 2. The
	// store must open at 1 and replay block 2. The rewind goes into the working dir, which is what a
	// torn commit leaves behind and what the next open actually reads: tampering with the snapshot
	// instead has no effect, because SNAPSHOT_BASE still matches and the working dir is reused.
	rewindVersionRecords(t, s1, 1, accountDBDir)
	require.NoError(t, s1.Close())

	// Reopen -- catchup should replay version 2 from WAL
	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbDir

	s2, err := newCommitStoreWithWAL(t.Context(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, wantRoot, rootHash(s2),
		"replaying an already-applied block must reproduce the same global root")
	for _, dbDir := range dataDBDirs {
		require.Equal(t, wantPerDB[dbDir], s2.perDBWorkingLtHash[dbDir].Checksum(),
			"%s per-DB root must be bit-identical after replay", dbDir)
	}
	require.Equal(t, int64(2), s2.Version())
	verifyPerDBLtHash(t, s2)
	verifyLtHashAtHeight(t, s2, 2)
}

// Test: Per-DB full scan verification after restart.
func TestPerDBLtHashPersistenceAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	for i := byte(1); i <= 10; i++ {
		commitMixedState(t, s1, i)
	}
	verifyPerDBLtHash(t, s1)
	require.NoError(t, s1.Close())

	// Reopen and verify
	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbDir

	s2, err := newCommitStoreWithWAL(t.Context(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
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
		miscKey := append([]byte{0x09}, addr[:]...)

		cs1 := namedCS(
			noncePair(addr, uint64(i)),
			storagePair(addr, slot, []byte{byte(i), 0xAA}),
		)
		cs2 := makeChangeSet(miscKey, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1, cs2}))
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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyPerDBLtHash(t, s)

	for i := 16; i <= 18; i++ {
		addr := addrN(byte(i - 15))
		slot := slotN(byte(i - 15))
		cs := namedCS(storagePair(addr, slot, []byte{byte(i), 0xBB}))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	for i := 19; i <= 20; i++ {
		addr := addrN(byte(i - 15))
		slot := slotN(byte(i - 15))
		cs := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
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
	for _, dbDir := range []string{accountDBDir, codeDBDir, storageDBDir, miscDBDir} {
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	require.NoError(t, s1.outOfBandSnapshot())

	commitMixedState(t, s1, 3)
	commitMixedState(t, s1, 4)
	commitMixedState(t, s1, 5)
	verifyPerDBLtHash(t, s1)

	expectedPerDB := make(map[string][32]byte, 4)
	for dbDir, h := range s1.perDBWorkingLtHash {
		expectedPerDB[dbDir] = h.Checksum()
	}
	require.NoError(t, s1.Close())

	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbDir

	s2, err := newCommitStoreWithWAL(t.Context(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
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
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{namedCS()}))
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitMixedState(t, s, 1)
	commitMixedState(t, s, 2)
	commitMixedState(t, s, 3)
	require.NoError(t, s.outOfBandSnapshot())

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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitMixedState(t, s, 1)
	commitMixedState(t, s, 2)

	dbInstances := make(map[string]types.KeyValueDB, len(dataDBDirs))
	for _, dir := range dataDBDirs {
		dbInstances[dir] = s.rawDBFor(dir)
	}
	requireFlushedToDisk(t, s)
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	var pairs []*proto.KVPair
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
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	verifyPerDBLtHash(t, s)
	verifyLtHashAtHeight(t, s, 1)
	require.NoError(t, s.Close())
}

func TestPerDBLtHashPartialKeyTypeOperations(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))

	// Write only storage keys: other DBs' per-DB LtHash should remain zero.
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	zeroChecksum := lthash.New().Checksum()
	require.NotEqual(t, zeroChecksum, s.perDBWorkingLtHash[storageDBDir].Checksum(),
		"storageDB hash should be non-zero")
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[accountDBDir].Checksum(),
		"accountDB hash should remain zero")
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[codeDBDir].Checksum(),
		"codeDB hash should remain zero")
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[miscDBDir].Checksum(),
		"miscDB hash should remain zero")
}

func TestPerDBLtHashDeleteLastKeyZerosHash(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x02)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))

	cs := makeChangeSet(key, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	nonZeroHash := s.perDBWorkingLtHash[storageDBDir].Checksum()
	zeroChecksum := lthash.New().Checksum()
	require.NotEqual(t, zeroChecksum, nonZeroHash)

	// Delete the only storage key.
	delCS := makeChangeSet(key, nil, true)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{delCS}))
	commitAndCheck(t, s)

	// After deleting all keys from a DB, its hash should return to zero.
	require.Equal(t, zeroChecksum, s.perDBWorkingLtHash[storageDBDir].Checksum(),
		"storageDB hash should be zero after deleting all keys")

	// Verify via full scan.
	scanHash := testFullScanDBLtHash(t, s.rawDBFor(storageDBDir))
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
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(storageKey, padLeft32(0x33), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)
	verifySumInvariant("after account add")

	// Operation 3: Add code.
	cs3 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			codePair(addr, []byte{0x60, 0x60}),
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s)
	verifySumInvariant("after code add")

	// Operation 4: Update storage.
	cs4 := makeChangeSet(storageKey, padLeft32(0x44), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs4}))
	commitAndCheck(t, s)
	verifySumInvariant("after storage update")

	// Operation 5: Delete storage.
	cs5 := makeChangeSet(storageKey, nil, true)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs5}))
	commitAndCheck(t, s)
	verifySumInvariant("after storage delete")

	// Operation 6: Delete account (nonce + codehash).
	cs6 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Delete: true},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs6}))
	commitAndCheck(t, s)
	verifySumInvariant("after account delete")

	// Operation 7: Delete code.
	cs7 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs7}))
	commitAndCheck(t, s)
	verifySumInvariant("after code delete")

	// Operation 8: Empty commit.
	commitAndCheck(t, s)
	verifySumInvariant("after empty commit")
}

// Stores flush independently, so a crash can leave them at genuinely different heights — not merely
// disagreeing with the watermark, but with each other. Replay must start from the lowest of them and
// apply each block only to the stores missing it, or the ones that already have it fold that block
// into their LtHash twice.
//
// The skew is forged by rewinding one data database's recorded height on disk while the store is
// closed, which is what a lost flush of that database looks like on the next open.
func TestPerDBLtHashLevelsUpStoresAtDifferentHeights(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s1.LoadLatest())

	commitMixedState(t, s1, 1)
	commitMixedState(t, s1, 2)
	commitMixedState(t, s1, 3)
	verifyPerDBLtHash(t, s1)
	wantPerDB := make(map[string]*lthash.LtHash, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		wantPerDB[dbDir] = s1.perDBWorkingLtHash[dbDir].Clone()
	}
	wantGlobal := s1.workingLtHash.Clone()
	require.NoError(t, s1.Close())

	// Rewind only the storage database's recorded height, leaving the others at 3. On reopen the stores
	// are at {storage: 1, others: 3}, so the store derives version 1 and replays 2 and 3 into databases
	// that already hold them.
	// The working directory, not the snapshot — see TestPerDBLtHashSkewRecovery.
	storageCfg := pebbledb.DefaultConfig()
	storageCfg.DataDir = filepath.Join(dbDir, workingDirName, storageDBDir)
	resolved := resolveConfig(cfg)
	require.Equal(t, resolved.StorageDBConfig.DataDir, storageCfg.DataDir,
		"the forged skew must target the directory the store opens, or this test proves nothing")
	storageCfg.EnableMetrics = false
	db, err := pebbledb.Open(t.Context(), &storageCfg)
	require.NoError(t, err)
	require.NoError(t, db.Set(ktype.MetaVersionKey, versionToBytes(1), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, s2.Close()) }()
	require.NoError(t, s2.LoadLatest())

	// Every store ends level, at the height they collectively reached before the forged skew.
	require.Equal(t, int64(3), s2.Version())
	for _, dbDir := range dataDBDirs {
		require.True(t, wantPerDB[dbDir].Equal(s2.perDBWorkingLtHash[dbDir]),
			"per-DB LtHash for %s must be restored exactly, not double-mixed:\n  want: %x\n  got:  %x",
			dbDir, wantPerDB[dbDir].Checksum(), s2.perDBWorkingLtHash[dbDir].Checksum())
	}
	require.True(t, wantGlobal.Equal(s2.workingLtHash), "global LtHash must be restored exactly")
	verifyPerDBLtHash(t, s2)
}
