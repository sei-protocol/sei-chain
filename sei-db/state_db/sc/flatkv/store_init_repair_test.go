package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// SetInitialVersion and FinalizeImport each stamp the four data DBs' watermarks non-atomically, so a
// crash partway through leaves the DBs disagreeing about a version that never entered the WAL. These
// tests cover the whole input domain the open path classifies from: the version and root records, and
// whether any DB holds data. Several of the states cannot be produced through the public API, so they
// are stamped directly — see stampSeedRecords / stripMetaRecords / writeRawDataKey.

// bankPair returns a changeset for a non-EVM module, which routes to the misc DB.
func bankPair(key, value []byte) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      keys.BankStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: value}}},
	}
}

// reopenStore closes s and opens a fresh store over the same directory.
func reopenStore(t *testing.T, s *CommitStore, cfg *config.Config) *CommitStore {
	t.Helper()
	require.NoError(t, s.Close())
	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	return reopened
}

// requireAllDataDBsAt asserts every data DB records version.
func requireAllDataDBsAt(t *testing.T, s *CommitStore, version int64) {
	t.Helper()
	for _, ndb := range s.namedDataDBs() {
		meta, err := loadLocalMeta(ndb.db)
		require.NoError(t, err)
		require.Equal(t, version, meta.CommittedVersion, "%s version record", ndb.dir)
	}
}

// TestCheckDataDBAlignmentOutcomes pins the classifier's three outcomes directly, since every branch
// of the open path turns on which one it returns.
func TestCheckDataDBAlignmentOutcomes(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.LoadLatest())

	interrupted, err := s.checkDataDBAlignment()
	require.NoError(t, err, "a fresh store's DBs all report 0")
	require.False(t, interrupted)

	// An initialization that stamped one DB and stopped. Nothing holds data, so it is discardable.
	s.localMeta[accountDBDir].CommittedVersion = 99
	interrupted, err = s.checkDataDBAlignment()
	require.Error(t, err)
	require.True(t, interrupted)
	require.ErrorContains(t, err, "no data DB holds any data")

	// The same skew with a row on disk. The verdict must flip: this one cannot be discarded.
	writeRawDataKey(t, s, storageDBDir, storagePhysKey(addrN(0x01), slotN(0x01)), padLeft32(0xAA))
	interrupted, err = s.checkDataDBAlignment()
	require.Error(t, err)
	require.False(t, interrupted, "data on disk must never be reported as discardable")
	require.ErrorContains(t, err, storageDBDir)
}

// TestUntouchedDataDBsOpenAtZero is the baseline of the classification: four DBs that have never had
// metadata written agree at 0, so nothing is misaligned and no repair is attempted.
func TestUntouchedDataDBsOpenAtZero(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.LoadLatest())
	require.Equal(t, int64(0), s.Version())
	requireAllDataDBsAt(t, s, 0)
}

// TestTornSeedIsDiscardedAndCanBeReseeded is the regression test for the bug. A seed that stamped some
// DBs and not others left a store that could never open again: the versions disagree, the WAL is empty
// so catchup reconciles nothing, and the alignment check refused. Because no DB holds data, the store
// is discarded and reopens as never-initialized, which is the state SetInitialVersion requires.
func TestTornSeedIsDiscardedAndCanBeReseeded(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	// Two of the four writes landed. No snapshot was written, so the baseline is still snapshot-0.
	stampSeedRecords(t, s, 99, accountDBDir, codeDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest(), "an interrupted seed must not be fatal")
	require.Equal(t, int64(0), reopened.Version(), "the discarded store reopens as never-initialized")
	requireAllDataDBsAt(t, reopened, 0)

	// The caller re-seeds, and the version it asks for need not be the one that was interrupted.
	require.NoError(t, reopened.SetInitialVersion(200))
	require.Equal(t, int64(199), reopened.Version())
	require.NoError(t, reopened.CommitBlock(200, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte("v"))}))
	require.Equal(t, int64(200), reopened.Version())
}

// TestCompletedSeedSurvivesReopen guards the other side of the discard: a seed that stamped every DB
// leaves them aligned, so the store must reopen at the seeded version with its records intact.
func TestCompletedSeedSurvivesReopen(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	require.NoError(t, s.SetInitialVersion(100))
	require.Equal(t, int64(99), s.Version())

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest())
	require.Equal(t, int64(99), reopened.Version(), "a completed seed must not be discarded")
	requireAllDataDBsAt(t, reopened, 99)
}

// TestTornSeedHoldingDataRefuses is the boundary that separates a lossless discard from data loss.
// The watermarks look exactly like an interrupted seed, but one DB holds a row, so discarding would
// destroy it. The store must refuse instead.
func TestTornSeedHoldingDataRefuses(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	stampSeedRecords(t, s, 99, accountDBDir, codeDBDir)
	writeRawDataKey(t, s, storageDBDir, storagePhysKey(addrN(0x01), slotN(0x01)), padLeft32(0xAA))

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	err = reopened.LoadLatest()
	require.Error(t, err, "a store holding data must never be discarded")
	require.NotContains(t, err.Error(), "no data DB holds any data")
	require.ErrorContains(t, err, storageDBDir, "the message must name the DB that holds data")
}

// TestOrphanedDataOutsideTheWALRefuses covers the state an interrupted state-sync restore and a live
// store that lost one DB's metadata both present as: one DB holds data that no metadata accounts for
// and that the WAL cannot replay, while the others carry watermarks. An import writes its rows
// outside the WAL, which is why replay cannot rebuild the missing metadata the way it does after a
// torn commit. The two causes are not distinguishable and discarding would destroy a live store's
// rows, so this refuses. Loosening it to a repair is the mistake this test exists to catch.
func TestOrphanedDataOutsideTheWALRefuses(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	// Rows written straight into misc, as an import does, plus watermarks on the DBs that were
	// finalized before the crash. The WAL never saw any of it.
	writeRawDataKey(t, s, miscDBDir, []byte(keys.BankStoreKey+"/k"), []byte("v"))
	stampSeedRecords(t, s, 1, accountDBDir, codeDBDir, storageDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	err = reopened.LoadLatest()
	require.Error(t, err, "data the WAL cannot account for must refuse, not be discarded")
	require.NotContains(t, err.Error(), "no data DB holds any data")
	require.ErrorContains(t, err, miscDBDir, "the message must name the DB that holds data")
}

// TestTornCommitStillRecoversFromWAL guards the ordinary case against the new classification: a DB
// whose version record was rewound, with its root intact, is brought forward by replay and the store
// opens aligned. Nothing is discarded and nothing refuses.
func TestTornCommitStillRecoversFromWAL(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	require.NoError(t, s.CommitBlock(1, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte("v"))}))
	requireAllDataDBsAt(t, s, 1)

	rewindVersionRecords(t, s, 0, miscDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest(), "the WAL still holds the block, so replay reconciles it")
	require.Equal(t, int64(1), reopened.Version())
	requireAllDataDBsAt(t, reopened, 1)
	require.NoError(t, VerifyLtHash(reopened))
}

// TestLostVersionRecordWithMetadataRefuses pins the read-time half of the co-presence invariant. A DB
// that lost its version and root records but kept its per-module records cannot be reported as
// never-written: the maintained hashes are updated by unmixing the old value and mixing the new, so
// replaying a block whose rows are already on disk cancels out and leaves them uncounted. The store
// would align its versions and open with a root that no longer describes its contents.
func TestLostVersionRecordWithMetadataRefuses(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	require.NoError(t, s.CommitBlock(1, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte("v"))}))

	// The per-module records under _meta/x:bank/ survive, which is what makes this distinguishable
	// from a DB that was never written.
	stripMetaRecords(t, s, miscDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	err = reopened.LoadLatest()
	require.Error(t, err, "a DB that lost its version record must not be read as never-written")
	require.ErrorContains(t, err, "lost its version record")
}

// TestIdentityRootsAtNonZeroVersionOpen pins that "every root is the identity" is not by itself read
// as an uninitialized store. Deleting every key returns the roots to the identity while the version
// records stay where they are, and such a store must open untouched.
func TestIdentityRootsAtNonZeroVersionOpen(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	require.NoError(t, s.CommitBlock(1, []*proto.NamedChangeSet{
		makeChangeSet(evmStorageKey(addrN(0x01), slotN(0x01)), padLeft32(0xAA), false),
	}))
	require.NoError(t, s.CommitBlock(2, []*proto.NamedChangeSet{
		makeChangeSet(evmStorageKey(addrN(0x01), slotN(0x01)), nil, true),
	}))
	require.True(t, s.committedLtHash.IsZero(), "fixture precondition: the store root is the identity")

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest())
	require.Equal(t, int64(2), reopened.Version())
	requireAllDataDBsAt(t, reopened, 2)
}

// TestReadOnlyCloneRefusesInterruptedInitialization pins that only the mutable open path repairs. A
// clone does not own the data directory, so a torn baseline must surface as an error and leave the
// working directory the live store is using alone.
func TestReadOnlyCloneRefusesInterruptedInitialization(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	require.NoError(t, s.SetInitialVersion(100))
	require.NoError(t, s.Close())

	// Tear the snapshot the clone will be built from, leaving the working dir intact so the live
	// store still opens.
	stripSnapshotMeta(t, cfg.DataDir, accountDBDir)

	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer reopened.Close()
	require.NoError(t, reopened.LoadLatest(), "the working dir is intact, so the live store opens")
	require.Equal(t, int64(99), reopened.Version())

	_, err = reopened.LoadVersionReadOnly(99)
	require.Error(t, err, "a clone over a torn baseline must refuse")

	workDir := filepath.Join(cfg.DataDir, workingDirName)
	require.DirExists(t, workDir, "a clone must never discard the live store's working dir")
	require.Equal(t, int64(99), reopened.Version(), "the live store must be undisturbed")
}

// TestDiscardIsNotRetriedForever pins that the repair happens once. When the torn state lives in the
// snapshot the working dir is re-cloned from, discarding reproduces it, and that has to surface
// rather than spin.
func TestDiscardIsNotRetriedForever(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	require.NoError(t, s.SetInitialVersion(100))
	require.NoError(t, s.Close())

	stripSnapshotMeta(t, cfg.DataDir, accountDBDir)
	// Drop SNAPSHOT_BASE so the working dir is re-cloned from the torn snapshot on open.
	require.NoError(t, os.Remove(filepath.Join(cfg.DataDir, workingDirName, snapshotBaseFile)))

	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer reopened.Close()

	err = reopened.LoadLatest()
	require.Error(t, err, "a discard that cannot fix the state must surface")
	require.ErrorContains(t, err, "no data DB holds any data",
		"the second attempt must surface the same verdict rather than loop")
}

// stripSnapshotMeta deletes the version and root records from one DB inside the current snapshot,
// which the store never opens for writing. The snapshot's PebbleDB is opened directly because the
// store deliberately has no API for mutating an immutable snapshot.
func stripSnapshotMeta(t *testing.T, flatkvDir string, dbDir string) {
	t.Helper()
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	require.NoError(t, err)

	cfg := pebbledb.DefaultConfig()
	cfg.DataDir = filepath.Join(snapDir, dbDir)
	cfg.EnableMetrics = false
	db, err := pebbledb.Open(t.Context(), &cfg)
	require.NoError(t, err)
	opts := dbtypes.WriteOptions{Sync: true}
	require.NoError(t, db.Delete(ktype.MetaVersionKey, opts))
	require.NoError(t, db.Delete(ktype.MetaLtHashKey, opts))
	require.NoError(t, db.Close())
}

// TestWriteLocalMetaRejectsNilHash pins the invariant every branch above reads from: a DB reports a
// version and a root together or neither. Without it, hydratePerDBState would substitute the identity
// for a DB that records no root, so a populated DB would contribute nothing to the store root.
func TestWriteLocalMetaRejectsNilHash(t *testing.T) {
	db := setupTestDB(t)
	batch := db.NewBatch()
	defer func() { _ = batch.Close() }()

	err := writeLocalMetaToBatch(batch, 7, nil, nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "no root hash")

	require.NoError(t, writeLocalMetaToBatch(batch, 7, lthash.New(), nil, nil))
}
