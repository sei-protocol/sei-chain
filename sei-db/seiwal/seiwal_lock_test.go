package seiwal

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
)

// TestFileLockPreventsSecondWAL verifies that a second WAL cannot open a directory a live WAL already owns.
func TestFileLockPreventsSecondWAL(t *testing.T) {
	dir := t.TempDir()
	w := openWAL(t, testConfig(dir))
	defer func() { require.NoError(t, w.Close()) }()

	_, err := NewWAL(testConfig(dir))
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)
}

// TestFileLockPreventsOfflineWhileLive verifies that the offline utilities fail fast while a WAL is live on
// the same directory, rather than mutating files the running WAL owns.
func TestFileLockPreventsOfflineWhileLive(t *testing.T) {
	dir := t.TempDir()
	w := openWAL(t, testConfig(dir))
	defer func() { require.NoError(t, w.Close()) }()

	_, _, _, err := GetRange(dir)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)

	err = PruneAfter(dir, 0)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)

	err = VerifyIntegrity(dir)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)

	err = DeleteAll(dir)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)
	require.DirExists(t, dir, "a wipe blocked by the lock must not have removed anything")
}

// TestFileLockReleasedOnClose verifies that Close releases the lock so a later WAL and the offline utilities
// can acquire the same directory.
func TestFileLockReleasedOnClose(t *testing.T) {
	dir := t.TempDir()

	w := openWAL(t, testConfig(dir))
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	// Every operation that takes the lock now succeeds because the lock was released by Close.
	ok, first, last, err := GetRange(dir)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)

	require.NoError(t, VerifyIntegrity(dir))
	require.NoError(t, PruneAfter(dir, 3))

	w2 := openWAL(t, testConfig(dir))
	require.NoError(t, w2.Close())

	require.NoError(t, DeleteAll(dir))
}

// TestDeleteAllRemovesDirectoryAndLockStillExcludes verifies that DeleteAll removes the whole directory,
// lock file included, and that the lock recreated by the next open still excludes a second owner. Asserting
// only that the directory is gone would pass even if the replacement lock file no longer excluded anything.
func TestDeleteAllRemovesDirectoryAndLockStillExcludes(t *testing.T) {
	dir := t.TempDir()

	w := openWAL(t, testConfig(dir))
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())
	require.FileExists(t, filepath.Join(dir, lockFileName))

	require.NoError(t, DeleteAll(dir))
	require.NoDirExists(t, dir)

	// Reopening recreates the directory and its lock file, and that fresh lock must exclude just as the
	// original did.
	w2 := openWAL(t, testConfig(dir))
	defer func() { require.NoError(t, w2.Close()) }()
	require.FileExists(t, filepath.Join(dir, lockFileName))

	_, err := NewWAL(testConfig(dir))
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)

	ok, _, _, err := w2.Bounds()
	require.NoError(t, err)
	require.False(t, ok, "WAL should be empty after DeleteAll")
}

// TestDeleteAllMissingDirIsNoop verifies DeleteAll is a clean no-op on a directory that does not exist, and
// that it does not create one on the way out.
func TestDeleteAllMissingDirIsNoop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never-created")
	require.NoError(t, DeleteAll(dir))
	require.NoDirExists(t, dir)
}

// TestFileLockSequentialOpenClose verifies that repeated open/close cycles succeed: the lock leaves no stale
// state that blocks a subsequent open.
func TestFileLockSequentialOpenClose(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		w := openWAL(t, testConfig(dir))
		require.NoError(t, w.Close())
	}
}

// TestFileLockFileIgnoredByScans verifies that the lock file does not interfere with directory recovery: a
// WAL that appended, closed, and reopened still reports the correct bounds despite wal.lock in the directory.
func TestFileLockFileIgnoredByScans(t *testing.T) {
	dir := t.TempDir()

	w := openWAL(t, testConfig(dir))
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	require.FileExists(t, filepath.Join(dir, lockFileName))

	w2 := openWAL(t, testConfig(dir))
	defer func() { require.NoError(t, w2.Close()) }()
	ok, first, last, err := w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)
}
