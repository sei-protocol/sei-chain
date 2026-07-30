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
