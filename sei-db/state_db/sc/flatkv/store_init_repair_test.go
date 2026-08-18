package flatkv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
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
func requireAllDataDBsAt(t *testing.T, s *CommitStore, version int64, because ...string) {
	t.Helper()
	for _, ndb := range selectDataDBs(t, s, nil) {
		meta, err := loadLocalMeta(ndb.db)
		require.NoError(t, err)
		require.Equal(t, version, meta.CommittedVersion, "%s version record %s", ndb.dir, strings.Join(because, " "))
	}
}

// TestRebuildTriggersOnlyAboveTheReachableVersion pins the condition the open path repairs on: a data
// DB recording a version that neither the snapshots nor the WAL can account for. Everything at or
// below that ceiling is left alone, including the ordinary disagreement a torn commit leaves.
func TestRebuildTriggersOnlyAboveTheReachableVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.LoadLatest())
	for i := int64(1); i <= 3; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}

	reachable, err := latestVersion(cfg.DataDir, s.wal)
	require.NoError(t, err)
	require.Equal(t, int64(3), reachable, "the WAL holds block 3, so 3 is reachable")

	// A DB behind the others is the ordinary torn commit: replay closes it, so no rebuild.
	s.localMeta[accountDBDir].CommittedVersion = 2
	require.NoError(t, s.rebuildIfAnyDataDBIsUnreachable())

	// A DB above the ceiling holds a block nothing can account for. The rebuild re-clones from the
	// current snapshot and stops there; bringing the store back up is replay's job.
	snapVersion, err := currentSnapshotVersion(cfg.DataDir)
	require.NoError(t, err)
	s.localMeta[accountDBDir].CommittedVersion = 4
	require.NoError(t, s.rebuildIfAnyDataDBIsUnreachable())
	requireAllDataDBsAt(t, s, snapVersion, "the working copy comes back from the snapshot")

	require.NoError(t, s.replayIntoMutableStore(0))
	require.Equal(t, int64(3), s.Version(), "replay then carries it to the WAL tail")
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

// TestTornSeedHoldingUnreachableDataIsRebuilt covers a torn seed that also left a row behind. The row
// was never committed — no snapshot and no WAL block accounts for it — so no consistent state includes
// it and the rebuild discards it along with the half-written watermarks. Data presence is not the
// discriminator; reachability is.
func TestTornSeedHoldingUnreachableDataIsRebuilt(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	stampSeedRecords(t, s, 99, accountDBDir, codeDBDir)
	writeRawDataKey(t, s, storageDBDir, storagePhysKey(addrN(0x01), slotN(0x01)), padLeft32(0xAA))

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest(), "an unreachable state must be repaired, not fatal")
	require.Equal(t, int64(0), reopened.Version())
	requireAllDataDBsAt(t, reopened, 0, "the working copy is rebuilt from the empty baseline")

	_, found := reopened.Get(keys.EVMStoreKey, evmStorageKey(addrN(0x01), slotN(0x01)))
	require.False(t, found, "the unreachable row is discarded with the rest of the working copy")
}

// TestInterruptedImportIsRebuilt covers the shape a state-sync restore leaves when it is interrupted:
// rows written outside the WAL, watermarks on the DBs that were finalized before the crash, and no
// snapshot vouching for any of it. Nothing reaches that height, so the store rebuilds to the baseline
// and comes up empty, ready for the restore to be redone — rather than refusing and taking the node
// down until an operator wipes the directory by hand.
func TestInterruptedImportIsRebuilt(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	writeRawDataKey(t, s, miscDBDir, []byte(keys.BankStoreKey+"/k"), []byte("v"))
	stampSeedRecords(t, s, 1, accountDBDir, codeDBDir, storageDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest())
	require.Equal(t, int64(0), reopened.Version())
	requireAllDataDBsAt(t, reopened, 0)
}

// TestCompletedImportSurvivesReopen is the other side of it: a finished import writes a snapshot at the
// restored height, and that snapshot is what makes the height reachable with an empty WAL. Without it
// the store would be rebuilt away.
func TestCompletedImportSurvivesReopen(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	require.NoError(t, s.SetInitialVersion(100))

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest())
	require.Equal(t, int64(99), reopened.Version(), "the snapshot vouches for the height")
	requireAllDataDBsAt(t, reopened, 99)
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

// TestReadOnlyCloneNeverRebuilds pins that only the mutable open path repairs. A clone does not own
// the data directory, so a torn baseline must surface as an error and leave the working directory the
// live store is using alone.
func TestReadOnlyCloneNeverRebuilds(t *testing.T) {
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

// TestUnrepairableSnapshotSurfaces pins that the rebuild is not a loop. When the damage lives in the
// snapshot the working copy is re-cloned from, rebuilding reproduces it, and the post-replay alignment
// check has to surface that rather than the open spinning on it.
func TestUnrepairableSnapshotSurfaces(t *testing.T) {
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
	require.Error(t, err, "damage inside the snapshot must surface rather than loop")
	require.ErrorContains(t, err, "did not bring every data DB to the same version")
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

// TestDataDBAheadOfWALTouchedByReplayIsRebuilt is the case that was silently mis-handled. Replay
// applying an older block to a DB that is past it rewrites that DB's version record downward while
// leaving the later block's rows in place, so the post-replay alignment check saw four agreeing DBs
// and passed — with the store's root summed over contents belonging to a height it no longer claimed.
//
// The fixture needs three things together, and dropping any one of them makes it pass without the fix:
// the ahead DB must physically hold the later block's rows, the replayed block must touch that DB so
// replay rewrites its record, and the later block must write a key the replayed block does not, so
// replay cannot overwrite the evidence. The assertion is against the root captured at the height the
// store ends up claiming.
func TestDataDBAheadOfWALTouchedByReplayIsRebuilt(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	require.NoError(t, s.CommitBlock(1, []*proto.NamedChangeSet{bankPair([]byte("a"), []byte{1})}))
	require.NoError(t, s.CommitBlock(2, []*proto.NamedChangeSet{bankPair([]byte("a"), []byte{2})}))
	wantRoot := bytes.Clone(rootHash(s))

	// Block 3 writes a different key, so replaying block 2 cannot overwrite it.
	require.NoError(t, s.CommitBlock(3, []*proto.NamedChangeSet{bankPair([]byte("b"), []byte{3})}))

	// Drop block 3 from the WAL, leaving misc holding its rows and recording version 3.
	require.NoError(t, s.wal.Close())
	require.NoError(t, statewal.PruneAfter(stateWALConfig(cfg.DataDir), 2))
	w, err := statewal.New(stateWALConfig(cfg.DataDir))
	require.NoError(t, err)
	s.wal = w

	// Only misc may be left ahead. A commit advances every DB's record, so the other three are put
	// back to 2 — where replay skips them — and account to 1 so replay has a block to apply at all.
	// Were any untouched DB left ahead, the post-replay check would catch it and the store would fail
	// loudly instead of taking the silent path this test exists to close.
	rewindVersionRecords(t, s, 2, codeDBDir, storageDBDir)
	rewindVersionRecords(t, s, 1, accountDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest(), "the unreachable block is discarded, not carried forward")
	require.Equal(t, int64(2), reopened.Version())
	requireAllDataDBsAt(t, reopened, 2)
	require.Equal(t, wantRoot, rootHash(reopened),
		"the root must be the one that belongs to the height the store reports")

	_, found := reopened.Get(keys.BankStoreKey, []byte("b"))
	require.False(t, found, "block 3's row must not survive at a store claiming version 2")
	require.NoError(t, VerifyLtHash(reopened))
}

// TestTwoDataDBsAheadAtDifferentVersions covers more than one DB above the reachable version, so the
// rebuild cannot be keyed on a single offending DB.
func TestTwoDataDBsAheadAtDifferentVersions(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	for i := int64(1); i <= 2; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}

	rewindVersionRecords(t, s, 5, miscDBDir)
	rewindVersionRecords(t, s, 4, storageDBDir)

	reopened := reopenStore(t, s, cfg)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest())
	require.Equal(t, int64(2), reopened.Version())
	requireAllDataDBsAt(t, reopened, 2)
	require.NoError(t, VerifyLtHash(reopened))
}
