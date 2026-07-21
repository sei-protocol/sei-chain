package operations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/stretchr/testify/require"
)

func TestSelectFlatKVSnapshotLatestFromCurrent(t *testing.T) {
	dbDir := t.TempDir()
	snapshot := flatkvSnapshotNameForTest(42)
	require.NoError(t, os.Mkdir(filepath.Join(dbDir, snapshot), 0o750))
	require.NoError(t, os.Symlink(snapshot, filepath.Join(dbDir, "current")))

	got, err := selectFlatKVSnapshot(dbDir, 0)
	require.NoError(t, err)
	require.Equal(t, snapshot, got)
}

func TestSelectFlatKVSnapshotExplicitHistoricalHeight(t *testing.T) {
	dbDir := t.TempDir()
	for _, v := range []int64{10, 20, 30} {
		require.NoError(t, os.Mkdir(filepath.Join(dbDir, flatkvSnapshotNameForTest(v)), 0o750))
	}

	got, err := selectFlatKVSnapshot(dbDir, 25)
	require.NoError(t, err)
	require.Equal(t, flatkvSnapshotNameForTest(20), got)

	_, err = selectFlatKVSnapshot(dbDir, 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no snapshot found")
}

func TestPrepareFlatKVToolingCloneHardlinksSnapshotAndCopiesChangelog(t *testing.T) {
	dbDir := t.TempDir()
	snapshot := flatkvSnapshotNameForTest(7)
	require.NoError(t, os.MkdirAll(filepath.Join(dbDir, snapshot, "account"), 0o750))
	srcSnapshotFile := filepath.Join(dbDir, snapshot, "account", "000001.sst")
	require.NoError(t, os.WriteFile(srcSnapshotFile, []byte("snapshot-data"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, snapshot, "LOCK"), []byte("lock"), 0o600))
	require.NoError(t, os.Symlink(snapshot, filepath.Join(dbDir, "current")))
	require.NoError(t, os.Mkdir(filepath.Join(dbDir, "changelog"), 0o750))
	srcChangelogFile := filepath.Join(dbDir, "changelog", "000001.log")
	require.NoError(t, os.WriteFile(srcChangelogFile, []byte("wal-data"), 0o600))

	cloneDir, err := prepareFlatKVToolingClone(dbDir, 0)
	require.NoError(t, err)
	defer os.RemoveAll(cloneDir) //nolint:errcheck // test cleanup

	target, err := os.Readlink(filepath.Join(cloneDir, "current"))
	require.NoError(t, err)
	require.Equal(t, snapshot, target)
	require.FileExists(t, filepath.Join(cloneDir, snapshot, "account", "000001.sst"))
	require.NoFileExists(t, filepath.Join(cloneDir, snapshot, "LOCK"))
	dstSnapshotFile := filepath.Join(cloneDir, snapshot, "account", "000001.sst")
	dstChangelogFile := filepath.Join(cloneDir, "changelog", "000001.log")
	require.FileExists(t, dstChangelogFile)

	srcSnapshotInfo, err := os.Stat(srcSnapshotFile)
	require.NoError(t, err)
	dstSnapshotInfo, err := os.Stat(dstSnapshotFile)
	require.NoError(t, err)
	require.True(t, os.SameFile(srcSnapshotInfo, dstSnapshotInfo), "immutable snapshot files should use hardlinks")

	srcChangelogInfo, err := os.Stat(srcChangelogFile)
	require.NoError(t, err)
	dstChangelogInfo, err := os.Stat(dstChangelogFile)
	require.NoError(t, err)
	require.False(t, os.SameFile(srcChangelogInfo, dstChangelogInfo), "live changelog files must be byte-copied")
}

func TestPrepareFlatKVToolingCloneRejectsBadChangelogPath(t *testing.T) {
	dbDir := t.TempDir()
	snapshot := flatkvSnapshotNameForTest(7)
	require.NoError(t, os.MkdirAll(filepath.Join(dbDir, snapshot, "account"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, snapshot, "account", "000001.sst"), []byte("snapshot-data"), 0o600))
	require.NoError(t, os.Symlink(snapshot, filepath.Join(dbDir, "current")))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, "changelog"), []byte("not-a-dir"), 0o600))

	_, err := prepareFlatKVToolingClone(dbDir, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "changelog path is not a directory")
}

func TestPrepareFlatKVToolingCloneReturnsChangelogStatErrors(t *testing.T) {
	dbDir := t.TempDir()
	snapshot := flatkvSnapshotNameForTest(7)
	require.NoError(t, os.MkdirAll(filepath.Join(dbDir, snapshot, "account"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, snapshot, "account", "000001.sst"), []byte("snapshot-data"), 0o600))
	require.NoError(t, os.Symlink(snapshot, filepath.Join(dbDir, "current")))
	require.NoError(t, os.Symlink("changelog", filepath.Join(dbDir, "changelog")))

	_, err := prepareFlatKVToolingClone(dbDir, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stat changelog")
}

func TestPrepareFlatKVToolingCloneMissingCurrentAndSnapshot(t *testing.T) {
	dbDir := t.TempDir()
	_, err := prepareFlatKVToolingClone(dbDir, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read current symlink")

	missingSnapshot := flatkvSnapshotNameForTest(9)
	require.NoError(t, os.Symlink(missingSnapshot, filepath.Join(dbDir, "current")))
	_, err = prepareFlatKVToolingClone(dbDir, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Contains(t, err.Error(), "clone aborted after")
}

func TestPrepareFlatKVToolingCloneRetriesENOENT(t *testing.T) {
	var attempts int
	cloneDir, err := prepareFlatKVToolingCloneWith(t.TempDir(), 0, func(string, int64) (string, error) {
		attempts++
		if attempts < maxCloneRetries {
			return "", fmt.Errorf("source vanished: %w", os.ErrNotExist)
		}
		return t.TempDir(), nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, cloneDir)
	require.Equal(t, maxCloneRetries, attempts)

	attempts = 0
	_, err = prepareFlatKVToolingCloneWith(t.TempDir(), 0, func(string, int64) (string, error) {
		attempts++
		return "", errors.New("permission denied")
	})
	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

// TestPrepareFlatKVToolingClonePlacesTempDirInsideDBDir locks in the fix for
// the tmpfs-fallback availability bug. The clone must live under dbDir itself
// so it shares the source snapshots' mounted filesystem even when dbDir is a
// dedicated mount point.
func TestPrepareFlatKVToolingClonePlacesTempDirInsideDBDir(t *testing.T) {
	store, dbDir := newDiskBackedFlatKVStore(t)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrN(0xA1), 1),
		}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, store.WriteSnapshot(""))
	require.NoError(t, store.Close())

	cloneDir, err := prepareFlatKVToolingClone(dbDir, 0)
	require.NoError(t, err)
	defer os.RemoveAll(cloneDir) //nolint:errcheck // test cleanup

	rel, err := filepath.Rel(dbDir, cloneDir)
	require.NoError(t, err)
	require.NotEqual(t, ".", rel)
	require.False(t, strings.HasPrefix(rel, ".."), "tooling clone must be created inside dbDir to stay on dbDir's mounted filesystem")
	require.Contains(t, filepath.Base(cloneDir), ".seidb-flatkv-tool-")
}

// TestPrepareFlatKVToolingCloneDetectsWALTruncationRace simulates the audited
// race: a live writer rolls a new snapshot between our snapshot clone and the
// WAL clone, then tryTruncateWAL drops WAL entries past our snapshot version.
// The cloned WAL would skip versions during catchup; the clone path must
// detect the gap and surface it as a retryable errSourceChurning.
func TestPrepareFlatKVToolingCloneDetectsWALTruncationRace(t *testing.T) {
	store, dbDir := newDiskBackedFlatKVStore(t)

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrN(0xA1), 1),
		}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, store.WriteSnapshot(""))

	for i := byte(2); i <= 5; i++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name: keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				noncePair(addrN(i), uint64(i)),
			}},
		}}))
		_, err := store.Commit(store.Version() + 1)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	walDir := filepath.Join(dbDir, "changelog")
	walLog, err := wal.NewChangelogWAL(walDir, wal.Config{})
	require.NoError(t, err)
	first, err := walLog.FirstOffset()
	require.NoError(t, err)
	last, err := walLog.LastOffset()
	require.NoError(t, err)

	var v4Off uint64
	require.NoError(t, walLog.Replay(first, last, func(off uint64, entry proto.ChangelogEntry) error {
		if entry.Version == 4 && v4Off == 0 {
			v4Off = off
		}
		return nil
	}))
	require.Greater(t, v4Off, uint64(0), "WAL should contain a v4 entry")
	require.NoError(t, walLog.TruncateBefore(v4Off))
	require.NoError(t, walLog.Close())

	_, err = prepareFlatKVToolingClone(dbDir, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, errSourceChurning,
		"clone must surface a retryable error when the WAL no longer covers snapshotVersion+1")
}

func TestOpenFlatKVReadOnlyLatestAndHistoricalHeight(t *testing.T) {
	store, dbDir := newDiskBackedFlatKVStore(t)
	addrA := addrN(0xA1)
	addrB := addrN(0xB2)

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrA, 1),
		}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, store.WriteSnapshot(""))

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrB, 2),
		}},
	}}))
	_, err = store.Commit(store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, store.WriteSnapshot(""))
	require.NoError(t, store.Close())

	latest, err := openFlatKVReadOnly(dbDir, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), latest.Version())
	_, found := latest.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addrB[:]))
	require.True(t, found)
	require.NoError(t, latest.Close())

	historical, err := openFlatKVReadOnly(dbDir, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), historical.Version())
	_, found = historical.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addrA[:]))
	require.True(t, found)
	_, found = historical.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addrB[:]))
	require.False(t, found)
	require.NoError(t, historical.Close())
}

func TestOpenFlatKVReadOnlyAfterSetInitialVersion(t *testing.T) {
	store, dbDir := newDiskBackedFlatKVStore(t)
	addr := addrN(0xC3)

	require.NoError(t, store.SetInitialVersion(100))
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 7),
		}},
	}}))
	v, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(100), v)
	require.NoError(t, store.Close())

	latest, err := openFlatKVReadOnly(dbDir, 0)
	require.NoError(t, err)
	require.Equal(t, int64(100), latest.Version())
	_, found := latest.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.NoError(t, latest.Close())
}

func newDiskBackedFlatKVStore(t *testing.T) (*flatkv.CommitStore, string) {
	t.Helper()

	cfg := flatkvconfig.DefaultTestConfig(t)
	store, err := flatkv.NewCommitStore(context.Background(), cfg)
	require.NoError(t, err)
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	return store, cfg.DataDir
}

func flatkvSnapshotNameForTest(version int64) string {
	return fmt.Sprintf("%s%020d", flatkvSnapshotPrefix, version)
}
