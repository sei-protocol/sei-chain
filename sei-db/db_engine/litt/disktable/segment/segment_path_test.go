package segment

import (
	"fmt"
	"path"
	"testing"

	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/stretchr/testify/require"
)

func TestSegmentPathWithSnapshotDir(t *testing.T) {
	dir := t.TempDir()

	snapshotDir := path.Join(dir, "snapshot")
	roots := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		roots = append(roots, path.Join(dir, fmt.Sprintf("%d", i)))
	}

	tableName := "table"

	segmentPaths, err := BuildSegmentPaths(roots, snapshotDir, tableName)
	require.NoError(t, err)

	for i, segmentPath := range segmentPaths {

		require.True(t, segmentPath.snapshottingEnabled())
		require.Equal(t, path.Join(roots[i], tableName, SegmentDirectory), segmentPath.SegmentDirectory())
		require.Equal(t, path.Join(roots[i], tableName, HardLinkDirectory), segmentPath.HardlinkPath())
		require.Equal(t, path.Join(snapshotDir, tableName, SegmentDirectory), segmentPath.SoftlinkPath())

		err = segmentPath.MakeDirectories(false)
		require.NoError(t, err)

		exists, err := util.Exists(segmentPath.SegmentDirectory())
		require.NoError(t, err)
		require.True(t, exists, "Segment directory should exist: %s", segmentPath.SegmentDirectory())

		exists, err = util.Exists(segmentPath.HardlinkPath())
		require.NoError(t, err)
		require.True(t, exists, "Hardlink path should exist: %s", segmentPath.HardlinkPath())

		exists, err = util.Exists(segmentPath.SoftlinkPath())
		require.NoError(t, err)
		require.True(t, exists, "Softlink path should exist: %s", segmentPath.SoftlinkPath())
	}
}

func TestSegmentPathWithoutSnapshotDir(t *testing.T) {
	dir := t.TempDir()

	roots := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		roots = append(roots, path.Join(dir, fmt.Sprintf("%d", i)))
	}

	tableName := "table"

	segmentPaths, err := BuildSegmentPaths(roots, "", tableName)
	require.NoError(t, err)

	for i, segmentPath := range segmentPaths {

		require.False(t, segmentPath.snapshottingEnabled())
		require.Equal(t, path.Join(roots[i], tableName, SegmentDirectory), segmentPath.SegmentDirectory())
		require.Equal(t, "", segmentPath.HardlinkPath())
		require.Equal(t, "", segmentPath.SoftlinkPath())

		err = segmentPath.MakeDirectories(false)
		require.NoError(t, err)

		exists, err := util.Exists(segmentPath.SegmentDirectory())
		require.NoError(t, err)
		require.True(t, exists, "Segment directory should exist: %s", segmentPath.SegmentDirectory())

		// Since we are not snapshotting, we shouldn't create this directory.
		exists, err = util.Exists(segmentPath.HardlinkPath())
		require.NoError(t, err)
		require.False(t, exists, "Hardlink path should exist: %s", segmentPath.HardlinkPath())
	}
}
