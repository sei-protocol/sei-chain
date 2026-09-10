package evm

import (
	"os"
	"path/filepath"
	"testing"

	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
	"github.com/stretchr/testify/require"
)

func writeMarkedDir(t *testing.T, path, marker string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "marker"), []byte(marker), 0o600))
}

func markerOf(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(path, "marker"))
	require.NoError(t, err)
	return string(got)
}

// requireNoLeftovers asserts nothing a restore stages beside dst outlived the heal.
func requireNoLeftovers(t *testing.T, dst string) {
	t.Helper()
	for _, leftover := range []string{dst + restoreTmpSuffix, dst + restoreBakSuffix} {
		_, err := os.Stat(leftover)
		require.True(t, os.IsNotExist(err), "%q is a full copy of the store and must not survive", leftover)
	}
}

// A restore interrupted between the two renames that swap the new copy in leaves no directory at all,
// which is otherwise indistinguishable from a store that was never written: the open creates an empty
// one, the head reads 0, and a catch-up stamps its target over almost no state.
func TestHealInterruptedRestore(t *testing.T) {
	t.Run("promotes the staged copy over the displaced one", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst+restoreTmpSuffix, "staged")
		writeMarkedDir(t, dst+restoreBakSuffix, "displaced")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "staged", markerOf(t, dst),
			"landing on the snapshot is what the interrupted rewind was for")
		requireNoLeftovers(t, dst)
	})

	t.Run("falls back to the displaced copy", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst+restoreBakSuffix, "displaced")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "displaced", markerOf(t, dst))
	})

	t.Run("leaves a store that has never been restored absent", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")

		require.NoError(t, healInterruptedRestore(dst))

		_, err := os.Stat(dst)
		require.True(t, os.IsNotExist(err), "with nothing to promote the directory must not appear")
	})

	t.Run("leaves a store that is already there alone", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst, "live")
		writeMarkedDir(t, dst+restoreTmpSuffix, "staged")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "live", markerOf(t, dst))
		requireNoLeftovers(t, dst)
	})

	// Only a later restore of this same directory would clear a leftover, so a node that crashed once
	// and never rewinds again carries a second copy of the store for good.
	t.Run("clears a leftover the swap never consumed", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst, "live")
		writeMarkedDir(t, dst+restoreBakSuffix, "displaced")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "live", markerOf(t, dst))
		requireNoLeftovers(t, dst)
	})
}

// A rewind with nothing to land on must not clear the live databases. The store above the target still
// holds history; wiping it is not a rewind.
func TestRewindClosedStoreToRefusesWhenThereIsNoSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "db")
	writeMarkedDir(t, dir, "live")

	_, err := RewindClosedStoreTo(dir, t.TempDir(), false, 1)

	require.ErrorContains(t, err, "no snapshot at or below target")
	require.Equal(t, "live", markerOf(t, dir), "a refused rewind must leave the live store in place")
}

func TestResetClosedStore(t *testing.T) {
	t.Run("empties a unified store and every snapshot", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		root := t.TempDir()
		writeMarkedDir(t, dir, "live")
		writeSnapshots(t, root, 10, 20)

		require.NoError(t, ResetClosedStore(dir, root, false))

		require.NoDirExists(t, dir, "the next open must create a store at version 0")
		requireNoSnapshots(t, root)
	})

	t.Run("empties every sub-DB of a separate-DB store", func(t *testing.T) {
		dir := t.TempDir()
		for _, storeType := range AllEVMStoreTypes() {
			writeMarkedDir(t, subDBPath(dir, storeType), "live")
		}

		require.NoError(t, ResetClosedStore(dir, t.TempDir(), true))

		for _, storeType := range AllEVMStoreTypes() {
			require.NoDirExists(t, subDBPath(dir, storeType),
				"a sub-DB left behind would read as state above a store the replay rebuilds from block 1")
		}
	})

	// promoteInterruptedRestore moves a leftover into an absent directory, so one surviving the reset
	// would have the next open resurrect the store this emptied.
	t.Run("clears the copies an interrupted restore staged", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dir, "live")
		writeMarkedDir(t, dir+restoreTmpSuffix, "staged")
		writeMarkedDir(t, dir+restoreBakSuffix, "displaced")

		require.NoError(t, ResetClosedStore(dir, t.TempDir(), false))

		require.NoDirExists(t, dir)
		requireNoLeftovers(t, dir)
		require.NoError(t, healInterruptedRestore(dir))
		require.NoDirExists(t, dir, "the heal on the next open must have nothing to promote")
	})

	t.Run("leaves a store that has never been written empty", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")

		require.NoError(t, ResetClosedStore(dir, filepath.Join(t.TempDir(), "snapshots"), false))

		require.NoDirExists(t, dir)
	})
}

// A separate-DB store is emptied one sub-DB at a time, so an interruption partway leaves the rest still
// holding the branch the reset was discarding. The head is the lowest of them, so the recreated empty
// sub-DB has the store read as new, and a caller that trusted it would leave those rows in place for a
// replay that only ever writes forward.
func TestHighestDBVersionSeesAnInterruptedReset(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.SeparateEVMSubDBs = true

	store, err := NewEVMStateStore(dir, cfg)
	require.NoError(t, err)
	require.Greater(t, len(store.managedDBs), 1)
	require.NoError(t, store.SetLatestVersion(5))
	require.NoError(t, store.Close())

	require.NoError(t, removePebbleDir(subDBPath(dir, AllEVMStoreTypes()[0])))

	reopened, err := NewEVMStateStore(dir, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	require.Zero(t, reopened.GetLatestVersion(), "the sub-DB the reset removed reopens empty")
	require.Equal(t, int64(5), reopened.HighestDBVersion(),
		"the sub-DBs it had not reached still record the block the reset was discarding")
}

func writeSnapshots(t *testing.T, root string, versions ...int64) {
	t.Helper()
	for _, version := range versions {
		writeMarkedDir(t, filepath.Join(root, sssnapshot.SnapshotDirName(version)), "snapshot")
	}
}

// requireNoSnapshots asserts the reset left nothing for a later rewind to land on: every snapshot of a
// store emptied to be replayed from block 1 belongs to the branch that reset abandoned.
func requireNoSnapshots(t *testing.T, root string) {
	t.Helper()
	versions, err := sssnapshot.ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Empty(t, versions)
}
