package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sscomposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/stretchr/testify/require"
)

func makeFlatKVSnapshots(t *testing.T, root string, heights ...int64) {
	t.Helper()
	for _, h := range heights {
		require.NoError(t, os.MkdirAll(filepath.Join(root, fmt.Sprintf("%s%020d", flatKVArchiveSnapshotPref, h)), 0o755))
	}
}

func makeSSCheckpoints(t *testing.T, stateStoreDir string, versions ...int64) {
	t.Helper()
	for _, v := range versions {
		dir := filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName, sscomposite.CheckpointDirName(v))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cosmos", "pebbledb"), 0o755))
	}
}

func TestSelectStateStoreSourcePrefersNewestCheckpoint(t *testing.T) {
	stateStoreDir := t.TempDir()
	makeSSCheckpoints(t, stateStoreDir, 100, 200)

	source, name, version, err := selectStateStoreSource(stateStoreDir)
	require.NoError(t, err)
	require.Equal(t, sscomposite.CheckpointDirName(200), name)
	require.Equal(t, int64(200), version)
	require.Equal(t,
		filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName, sscomposite.CheckpointDirName(200)),
		source)
}

func TestSelectStateStoreSourceFallsBackToLiveDir(t *testing.T) {
	stateStoreDir := t.TempDir()

	source, name, version, err := selectStateStoreSource(stateStoreDir)
	require.NoError(t, err)
	require.Empty(t, name)
	require.Zero(t, version)
	require.Equal(t, stateStoreDir, source)
}

func TestSelectFlatKVSnapshotAtMost(t *testing.T) {
	root := t.TempDir()
	makeFlatKVSnapshots(t, root, 100, 200, 300)

	// The state-store checkpoint's label bounds the FlatKV height from above:
	// pairing H <= label is what guarantees the restored query store is
	// complete up to the archive height.
	dir, name, height, err := selectFlatKVSnapshotAtMost(root, 250)
	require.NoError(t, err)
	require.Equal(t, int64(200), height)
	require.Equal(t, fmt.Sprintf("%s%020d", flatKVArchiveSnapshotPref, int64(200)), name)
	require.Equal(t, filepath.Join(root, name), dir)

	// Exact match is allowed.
	_, _, height, err = selectFlatKVSnapshotAtMost(root, 300)
	require.NoError(t, err)
	require.Equal(t, int64(300), height)

	// No snapshot at or below the checkpoint label: refuse rather than ship
	// an archive whose query store has holes below the archive height.
	_, _, _, err = selectFlatKVSnapshotAtMost(root, 50)
	require.Error(t, err)
}
