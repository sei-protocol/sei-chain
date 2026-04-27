package operations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
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

func TestPrepareFlatKVToolingCloneCopiesSnapshotAndChangelog(t *testing.T) {
	dbDir := t.TempDir()
	snapshot := flatkvSnapshotNameForTest(7)
	require.NoError(t, os.MkdirAll(filepath.Join(dbDir, snapshot, "account"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, snapshot, "account", "000001.sst"), []byte("snapshot-data"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, snapshot, "LOCK"), []byte("lock"), 0o600))
	require.NoError(t, os.Symlink(snapshot, filepath.Join(dbDir, "current")))
	require.NoError(t, os.Mkdir(filepath.Join(dbDir, "changelog"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dbDir, "changelog", "000001.log"), []byte("wal-data"), 0o600))

	cloneDir, err := prepareFlatKVToolingClone(dbDir, 0)
	require.NoError(t, err)
	defer os.RemoveAll(cloneDir) //nolint:errcheck // test cleanup

	target, err := os.Readlink(filepath.Join(cloneDir, "current"))
	require.NoError(t, err)
	require.Equal(t, snapshot, target)
	require.FileExists(t, filepath.Join(cloneDir, snapshot, "account", "000001.sst"))
	require.NoFileExists(t, filepath.Join(cloneDir, snapshot, "LOCK"))
	require.FileExists(t, filepath.Join(cloneDir, "changelog", "000001.log"))
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
	_, err := store.Commit()
	require.NoError(t, err)
	require.NoError(t, store.WriteSnapshot(""))

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrB, 2),
		}},
	}}))
	_, err = store.Commit()
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
