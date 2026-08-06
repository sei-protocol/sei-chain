package mvcc

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func openRollbackTestDBAt(t *testing.T, dir string) *Database {
	t.Helper()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend
	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db := store.(*Database)
	return db
}

func openRollbackTestDB(t *testing.T) *Database {
	t.Helper()
	db := openRollbackTestDBAt(t, t.TempDir())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func rollbackChangeset(store, key string, value []byte) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{
		{
			Name: store,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte(key), Value: value}},
			},
		},
	}
}

func TestRollbackRemovesVersionsAboveTarget(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}

	require.NoError(t, db.Rollback(3))
	require.Equal(t, int64(3), db.GetLatestVersion())

	val, err := db.Get("bank", 3, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{3}, val)

	require.NoError(t, db.ApplyChangesetAsync(4, rollbackChangeset("bank", "k", []byte{4})))
	require.NoError(t, db.ApplyChangesetAsync(5, rollbackChangeset("bank", "k", []byte{5})))
	db.WaitForPendingWrites()

	val, err = db.Get("bank", 5, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{5}, val)
}

func TestRollbackUndoesDeleteTombstone(t *testing.T) {
	db := openRollbackTestDB(t)

	require.NoError(t, db.ApplyChangesetAsync(1, rollbackChangeset("bank", "k", []byte("alive"))))
	require.NoError(t, db.ApplyChangesetAsync(2, rollbackChangeset("bank", "k", nil)))
	db.WaitForPendingWrites()

	val, err := db.Get("bank", 2, []byte("k"))
	require.NoError(t, err)
	require.Nil(t, val)

	require.NoError(t, db.Rollback(1))
	require.Equal(t, int64(1), db.GetLatestVersion())

	val, err = db.Get("bank", 1, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), val)

	require.NoError(t, db.ApplyChangesetAsync(2, rollbackChangeset("bank", "k", nil)))
	db.WaitForPendingWrites()
	val, err = db.Get("bank", 2, []byte("k"))
	require.NoError(t, err)
	require.Nil(t, val)
}

func TestRollbackCoverageFailureMutatesNothing(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()
	require.NoError(t, db.streamHandler.TruncateBefore(4))

	err := db.Rollback(1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "earliest recoverable target")
	require.Equal(t, int64(5), db.GetLatestVersion())

	val, getErr := db.Get("bank", 5, []byte("k"))
	require.NoError(t, getErr)
	require.Equal(t, []byte{5}, val)
}

func TestRollbackRejectsNonMonotonicChangelogWithoutMutation(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()

	// This is the ordering produced after --skip-state-store leaves SS at 5
	// while SC replays from an earlier height.
	require.NoError(t, db.ApplyChangesetAsync(3, rollbackChangeset("bank", "k", []byte("replayed-3"))))
	require.NoError(t, db.ApplyChangesetAsync(4, rollbackChangeset("bank", "k", []byte("replayed-4"))))
	db.WaitForPendingWrites()

	lastBefore, err := db.streamHandler.LastOffset()
	require.NoError(t, err)

	err = db.CheckRollbackCoverage(2)
	require.ErrorContains(t, err, "changelog is not monotonic")

	err = db.Rollback(2)
	require.ErrorContains(t, err, "changelog is not monotonic")
	require.Equal(t, int64(5), db.GetLatestVersion(), "preflight must fail before lowering the marker")

	lastAfter, err := db.streamHandler.LastOffset()
	require.NoError(t, err)
	require.Equal(t, lastBefore, lastAfter, "preflight must not truncate the changelog")

	val, err := db.Get("bank", 5, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{5}, val, "preflight must not delete MVCC rows")
}

// The shallowest target the coverage check accepts is one below the first
// retained changelog entry, which leaves nothing to keep in the log. That is
// the reachable case whenever the front of the changelog has been pruned, and
// it used to delete the versions and lower the marker before failing on a
// truncation the WAL would not accept.
func TestRollbackToVersionBelowEveryRetainedChangelogEntry(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()
	// First retained entry is now version 3, so 2 is the deepest legal target.
	require.NoError(t, db.streamHandler.TruncateBefore(3))

	require.NoError(t, db.Rollback(2))
	require.Equal(t, int64(2), db.GetLatestVersion())

	// Versions 3..5 are gone: a read at 5 sees the value written at 2.
	val, err := db.Get("bank", 5, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{2}, val)

	first, err := db.streamHandler.FirstOffset()
	require.NoError(t, err)
	last, err := db.streamHandler.LastOffset()
	require.NoError(t, err)
	require.Greater(t, first, last, "changelog must be empty so recovery replays nothing")

	// The emptied changelog must still accept writes, and they must be visible.
	require.NoError(t, db.ApplyChangesetAsync(3, rollbackChangeset("bank", "k", []byte{33})))
	db.WaitForPendingWrites()
	val, err = db.Get("bank", 3, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{33}, val)
}

// Truncating the changelog is what stops recovery from replaying the rolled-back
// versions straight back in on the next open, so pin that end to end.
func TestRollbackSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	db := openRollbackTestDBAt(t, dir)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()

	require.NoError(t, db.Rollback(3))
	require.NoError(t, db.Close())

	reopened := openRollbackTestDBAt(t, dir)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	require.Equal(t, int64(3), reopened.GetLatestVersion())
	val, err := reopened.Get("bank", 5, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{3}, val, "versions above the target must not come back")
}

// A rollback that fails partway leaves the marker lowered. Re-running it must
// finish the job rather than short-circuit on the marker and report success.
func TestRollbackRetryCompletesAfterMarkerAlreadyLowered(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()

	// Stand in for a rollback that lowered the marker and then failed.
	require.NoError(t, db.SetLatestVersion(3))

	require.NoError(t, db.Rollback(3))

	last, err := db.streamHandler.LastOffset()
	require.NoError(t, err)
	lastEntry, err := db.streamHandler.ReadAt(last)
	require.NoError(t, err)
	require.Equal(t, int64(3), lastEntry.Version, "retry must truncate the changelog it found above the target")

	val, err := db.Get("bank", 5, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte{3}, val, "retry must delete the versions above the target")
}

// ApplyChangesetAsync logs the genesis changeset as version 0 while the write
// path lands it at version 1, so undoing it has to target 1.
func TestRollbackToZeroUndoesGenesisChangeset(t *testing.T) {
	db := openRollbackTestDB(t)

	require.NoError(t, db.ApplyChangesetAsync(0, rollbackChangeset("bank", "k", []byte("genesis"))))
	db.WaitForPendingWrites()

	val, err := db.Get("bank", 1, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("genesis"), val)

	require.NoError(t, db.Rollback(0))

	val, err = db.Get("bank", 1, []byte("k"))
	require.NoError(t, err)
	require.Nil(t, val, "the genesis write lands at version 1 and must be undone there")
}

// Pruning skips a store whose dirty entry is missing, so a rollback must rewind
// the entry rather than drop it.
func TestRollbackRewindsStoreDirtyMarkerInsteadOfDropping(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()
	require.NoError(t, db.Rollback(3))

	dirty, ok := db.storeKeyDirty.Load("bank")
	require.True(t, ok, "store must stay visible to the prune scan after a rollback")
	require.Equal(t, int64(3), dirty)
}

func TestRollbackBelowEarliestVersionFailsWithoutMutation(t *testing.T) {
	db := openRollbackTestDB(t)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, db.ApplyChangesetAsync(version, rollbackChangeset("bank", "k", []byte{byte(version)})))
	}
	db.WaitForPendingWrites()
	require.NoError(t, db.SetEarliestVersion(4, false))

	err := db.Rollback(3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "earliest retained version is 4")
	require.Equal(t, int64(5), db.GetLatestVersion())
	require.Equal(t, int64(4), db.GetEarliestVersion())
}
