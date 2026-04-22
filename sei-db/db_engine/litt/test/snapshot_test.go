package test

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	rootPathCount := rand.Uint64Range(2, 5)
	rootPaths := make([]string, rootPathCount)
	for i := uint64(0); i < rootPathCount; i++ {
		rootPaths[i] = path.Join(testDirectory, fmt.Sprintf("root-%d", i))
	}

	snapshotDir := testDirectory + "/snapshot"

	// Configure the DB to enable snapshots.
	config, err := litt.DefaultConfig(rootPaths...)
	require.NoError(t, err)
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.ShardingFactor = uint32(rand.Uint64Range(rootPathCount, 2*rootPathCount))
	config.TargetSegmentFileSize = 100
	config.SnapshotDirectory = snapshotDir

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint64Range(2, 5)
	tables := make(map[string]litt.Table, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableName := fmt.Sprintf("table-%d", i)
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
	}

	// map from table name to keys to values
	expectedData := make(map[string]map[string][]byte)
	for _, table := range tables {
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Write some data into the DB.
	for i := 0; i < 1000; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		tableName := fmt.Sprintf("table-%d", tableIndex)
		table := tables[tableName]

		key := rand.String(32)
		value := rand.PrintableVariableBytes(1, 100)

		err = table.Put([]byte(key), value)
		require.NoError(t, err)

		expectedData[tableName][key] = value
	}

	// Flush all tables to ensure data is written to disk.
	for _, table := range tables {
		err = table.Flush()
		require.NoError(t, err)
	}

	// Now, let's compare the segment files in the snapshot directory with the segments in the regular directories.
	for tableName := range tables {

		segmentPaths, err := segment.BuildSegmentPaths(rootPaths, "", tableName)
		require.NoError(t, err)
		lowestSegmentIndex, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			segmentPaths,
			false,
			time.Now(),
			false,
			false)

		require.NoError(t, err)
		snapshotSegmentPath, err := segment.NewSegmentPath(snapshotDir, "", tableName)
		require.NoError(t, err)
		snapshotLowestSegmentIndex, snapshotHighestSegmentIndex, snapshotSegments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			[]*segment.SegmentPath{snapshotSegmentPath},
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		// Both the snapshot directory and the regular directories should agree on the lowest segment index.
		require.Equal(t, lowestSegmentIndex, snapshotLowestSegmentIndex)

		// The snapshot directory should have one fewer segments than the regular directories. The highest segment will
		// be mutable, and therefore won't appear in the snapshot.
		require.Equal(t, highestSegmentIndex-1, snapshotHighestSegmentIndex)
		require.Equal(t, len(segments)-1, len(snapshotSegments))

		// There should be a boundary file in the snapshot directory signaling the highest legal segment index in the
		// snapshot.
		boundaryFile, err := disktable.LoadBoundaryFile(disktable.UpperBound, path.Join(snapshotDir, tableName))
		require.NoError(t, err)
		require.True(t, boundaryFile.IsDefined())
		require.Equal(t, snapshotHighestSegmentIndex, boundaryFile.BoundaryIndex())

		for i := lowestSegmentIndex; i < highestSegmentIndex; i++ {
			regularSegment := segments[i]
			snapshotSegment := snapshotSegments[i]

			// The regular segment should know it is not a snapshot.
			snapshot, err := regularSegment.IsSnapshot()
			require.NoError(t, err)
			require.False(t, snapshot)

			// None of the regular segment files should be symlinks.
			for _, filePath := range regularSegment.GetFilePaths() {
				info, err := os.Lstat(filePath)
				require.NoError(t, err)
				require.False(t, info.Mode()&os.ModeSymlink != 0)
			}

			// The snapshot segment should realize that it is a snapshot.
			snapshot, err = snapshotSegment.IsSnapshot()
			require.NoError(t, err)
			require.True(t, snapshot)

			// All snapshot files should be symlinks.
			for _, filePath := range snapshotSegment.GetFilePaths() {
				info, err := os.Lstat(filePath)
				require.NoError(t, err)
				require.True(t, info.Mode()&os.ModeSymlink != 0)
			}

			// The keys should be the same in both segments.
			regularKeys, err := regularSegment.GetKeys()
			require.NoError(t, err)
			snapshotKeys, err := snapshotSegment.GetKeys()
			require.NoError(t, err)
			require.Equal(t, regularKeys, snapshotKeys)

			// The values should be present in both segments.
			for _, key := range regularKeys {
				regularValue, err := regularSegment.Read(key.Key, key.Address)
				require.NoError(t, err)

				snapshotValue, err := snapshotSegment.Read(key.Key, key.Address)
				require.NoError(t, err)

				require.Equal(t, regularValue, snapshotValue)
			}
		}
	}

	ok, err := errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)

	// Deleting the snapshot directory should not in any way cause issues with the database.
	err = db.Close()
	require.NoError(t, err)

	errorMonitor = util.NewErrorMonitor(ctx, logger, nil)

	err = os.RemoveAll(snapshotDir)
	require.NoError(t, err)

	// Reopen the database and ensure that it still works.
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)

	for tableName := range tables {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)

		// Ensure that the data is still present in the database.
		for key, expectedValue := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err)
			require.True(t, ok, "Expected key %s to be present in table %s", key, tableName)
			require.Equal(t, expectedValue, value)
		}
	}

	// Cleanup.
	err = db.Close()
	require.NoError(t, err)

	ok, err = errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)
}

// This test verifies that LittDB rebuilds the snapshot directory correctly every time it starts up.
func TestSnapshotRebuilding(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	rootPathCount := rand.Uint64Range(2, 5)
	rootPaths := make([]string, rootPathCount)
	for i := uint64(0); i < rootPathCount; i++ {
		rootPaths[i] = path.Join(testDirectory, fmt.Sprintf("root-%d", i))
	}

	snapshotDir := testDirectory + "/snapshot"

	// Configure the DB to enable snapshots.
	config, err := litt.DefaultConfig(rootPaths...)
	require.NoError(t, err)
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.ShardingFactor = uint32(rand.Uint64Range(rootPathCount, 2*rootPathCount))
	config.TargetSegmentFileSize = 100
	config.SnapshotDirectory = snapshotDir

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint64Range(2, 5)
	tables := make(map[string]litt.Table, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableName := fmt.Sprintf("table-%d", i)
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
	}

	// map from table name to keys to values
	expectedData := make(map[string]map[string][]byte)
	for _, table := range tables {
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Write some data into the DB.
	for i := 0; i < 1000; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		tableName := fmt.Sprintf("table-%d", tableIndex)
		table := tables[tableName]

		key := rand.String(32)
		value := rand.PrintableVariableBytes(1, 100)

		err = table.Put([]byte(key), value)
		require.NoError(t, err)

		expectedData[tableName][key] = value
	}

	// Flush all tables to ensure data is written to disk.
	for _, table := range tables {
		err = table.Flush()
		require.NoError(t, err)
	}

	// Delete all snapshot files with even indices.
	for tableName := range tables {
		require.NoError(t, err)
		snapshotSegmentPath, err := segment.NewSegmentPath(snapshotDir, "", tableName)
		require.NoError(t, err)
		snapshotLowestSegmentIndex, snapshotHighestSegmentIndex, snapshotSegments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			[]*segment.SegmentPath{snapshotSegmentPath},
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		for i := snapshotLowestSegmentIndex; i <= snapshotHighestSegmentIndex; i++ {
			if i%2 == 0 {
				for _, filePath := range snapshotSegments[i].GetFilePaths() {
					err = os.Remove(filePath)
					require.NoError(t, err, "Failed to remove file %s in snapshot directory", filePath)
				}
			}
		}
	}

	ok, err := errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)

	// Restart the DB.
	err = db.Close()
	require.NoError(t, err)

	errorMonitor = util.NewErrorMonitor(ctx, logger, nil)

	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)

	for tableName := range tables {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)

		// Ensure that the data is still present in the database.
		for key, expectedValue := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err)
			require.True(t, ok, "Expected key %s to be present in table %s", key, tableName)
			require.Equal(t, expectedValue, value)
		}
	}

	// Now, let's compare the segment files in the snapshot directory with the segments in the regular directories.
	// Our shenanigans above should have been fully fixed when the DB restarted.
	for tableName := range tables {

		segmentPaths, err := segment.BuildSegmentPaths(rootPaths, "", tableName)
		require.NoError(t, err)
		lowestSegmentIndex, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			segmentPaths,
			false,
			time.Now(),
			false,
			false)

		require.NoError(t, err)
		snapshotSegmentPath, err := segment.NewSegmentPath(snapshotDir, "", tableName)
		require.NoError(t, err)
		snapshotLowestSegmentIndex, snapshotHighestSegmentIndex, snapshotSegments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			[]*segment.SegmentPath{snapshotSegmentPath},
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		// Both the snapshot directory and the regular directories should agree on the lowest segment index.
		require.Equal(t, lowestSegmentIndex, snapshotLowestSegmentIndex)

		// The snapshot directory should have one fewer segments than the regular directories. The highest segment will
		// be mutable, and therefore won't appear in the snapshot.
		require.Equal(t, highestSegmentIndex-1, snapshotHighestSegmentIndex)
		require.Equal(t, len(segments)-1, len(snapshotSegments))

		// There should be a boundary file in the snapshot directory signaling the highest legal segment index in the
		// snapshot.
		boundaryFile, err := disktable.LoadBoundaryFile(disktable.UpperBound, path.Join(snapshotDir, tableName))
		require.NoError(t, err)
		require.True(t, boundaryFile.IsDefined())
		require.Equal(t, snapshotHighestSegmentIndex, boundaryFile.BoundaryIndex())

		for i := lowestSegmentIndex; i < highestSegmentIndex; i++ {
			regularSegment := segments[i]
			snapshotSegment := snapshotSegments[i]

			// The regular segment should know it is not a snapshot.
			snapshot, err := regularSegment.IsSnapshot()
			require.NoError(t, err)
			require.False(t, snapshot)

			// None of the regular segment files should be symlinks.
			for _, filePath := range regularSegment.GetFilePaths() {
				info, err := os.Lstat(filePath)
				require.NoError(t, err)
				require.False(t, info.Mode()&os.ModeSymlink != 0)
			}

			// The snapshot segment should realize that it is a snapshot.
			snapshot, err = snapshotSegment.IsSnapshot()
			require.NoError(t, err)
			require.True(t, snapshot)

			// All snapshot files should be symlinks.
			for _, filePath := range snapshotSegment.GetFilePaths() {
				info, err := os.Lstat(filePath)
				require.NoError(t, err)
				require.True(t, info.Mode()&os.ModeSymlink != 0)
			}

			// The keys should be the same in both segments.
			regularKeys, err := regularSegment.GetKeys()
			require.NoError(t, err)
			snapshotKeys, err := snapshotSegment.GetKeys()
			require.NoError(t, err)
			require.Equal(t, regularKeys, snapshotKeys)

			// The values should be present in both segments.
			for _, key := range regularKeys {
				regularValue, err := regularSegment.Read(key.Key, key.Address)
				require.NoError(t, err)

				snapshotValue, err := snapshotSegment.Read(key.Key, key.Address)
				require.NoError(t, err)

				require.Equal(t, regularValue, snapshotValue)
			}
		}
	}

	// Cleanup.
	err = db.Close()
	require.NoError(t, err)

	ok, err = errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)
}

// The DB should not attempt to rebuild snapshot files that are below the specified lower bound.
func TestSnapshotLowerBound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	rootPathCount := rand.Uint64Range(2, 5)
	rootPaths := make([]string, rootPathCount)
	for i := uint64(0); i < rootPathCount; i++ {
		rootPaths[i] = path.Join(testDirectory, fmt.Sprintf("root-%d", i))
	}

	snapshotDir := testDirectory + "/snapshot"

	// Configure the DB to enable snapshots.
	config, err := litt.DefaultConfig(rootPaths...)
	require.NoError(t, err)
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.ShardingFactor = uint32(rand.Uint64Range(rootPathCount, 2*rootPathCount))
	config.TargetSegmentFileSize = 100
	config.SnapshotDirectory = snapshotDir

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint64Range(2, 5)
	tables := make(map[string]litt.Table, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableName := fmt.Sprintf("table-%d", i)
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
	}

	// map from table name to keys to values
	expectedData := make(map[string]map[string][]byte)
	for _, table := range tables {
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Write some data into the DB.
	for i := 0; i < 1000; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		tableName := fmt.Sprintf("table-%d", tableIndex)
		table := tables[tableName]

		key := rand.String(32)
		value := rand.PrintableVariableBytes(1, 100)

		err = table.Put([]byte(key), value)
		require.NoError(t, err)

		expectedData[tableName][key] = value
	}

	// Flush all tables to ensure data is written to disk.
	for _, table := range tables {
		err = table.Flush()
		require.NoError(t, err)
	}

	// We are going to delete the lower half of snapshot files to simulate a "litt prune" command. The lower bound
	// file will be updated to signal that we do not want to reconstruct the deleted segments. We will delete all
	// other segments that have even indices, to verify that the DB does rebuild those segments.
	lowerBoundsByTable := make(map[string]uint32)
	for tableName := range tables {
		require.NoError(t, err)
		snapshotSegmentPath, err := segment.NewSegmentPath(snapshotDir, "", tableName)
		require.NoError(t, err)
		snapshotLowestSegmentIndex, snapshotHighestSegmentIndex, snapshotSegments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			[]*segment.SegmentPath{snapshotSegmentPath},
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		lowerBound := snapshotLowestSegmentIndex + (snapshotHighestSegmentIndex-snapshotLowestSegmentIndex)/2
		lowerBoundsByTable[tableName] = lowerBound
		boundaryFile, err := disktable.LoadBoundaryFile(disktable.LowerBound, path.Join(snapshotDir, tableName))
		require.NoError(t, err)
		err = boundaryFile.Update(lowerBound)
		require.NoError(t, err)

		for i := snapshotLowestSegmentIndex; i <= snapshotHighestSegmentIndex; i++ {
			if i%2 == 0 || i <= lowerBound {
				for _, filePath := range snapshotSegments[i].GetFilePaths() {
					err = os.Remove(filePath)
					require.NoError(t, err, "Failed to remove file %s in snapshot directory", filePath)
				}
			}
		}
	}

	ok, err := errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)

	// Restart the DB.
	err = db.Close()
	require.NoError(t, err)

	errorMonitor = util.NewErrorMonitor(ctx, logger, nil)

	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)

	for tableName := range tables {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)

		// Ensure that the data is still present in the database.
		for key, expectedValue := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err)
			require.True(t, ok, "Expected key %s to be present in table %s", key, tableName)
			require.Equal(t, expectedValue, value)
		}
	}

	// Now, let's compare the segment files in the snapshot directory with the segments in the regular directories.
	// Our shenanigans above should have been fully fixed for the files above the boundary, but no snapshots
	// should have been rebuilt for the files below or at the boundary.
	for tableName := range tables {

		segmentPaths, err := segment.BuildSegmentPaths(rootPaths, "", tableName)
		require.NoError(t, err)
		_, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			segmentPaths,
			false,
			time.Now(),
			false,
			false)

		require.NoError(t, err)
		snapshotSegmentPath, err := segment.NewSegmentPath(snapshotDir, "", tableName)
		require.NoError(t, err)
		snapshotLowestSegmentIndex, snapshotHighestSegmentIndex, snapshotSegments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			[]*segment.SegmentPath{snapshotSegmentPath},
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		// We shouldn't see snapshot files with an index less than or equal to the lower bound.
		require.Equal(t, lowerBoundsByTable[tableName]+1, snapshotLowestSegmentIndex)

		// The high segment index should be one less than the highest segment index in the regular directories.
		require.Equal(t, highestSegmentIndex-1, snapshotHighestSegmentIndex)

		// There should be a boundary file in the snapshot directory signaling the highest legal segment index in the
		// snapshot.
		boundaryFile, err := disktable.LoadBoundaryFile(disktable.UpperBound, path.Join(snapshotDir, tableName))
		require.NoError(t, err)
		require.True(t, boundaryFile.IsDefined())
		require.Equal(t, snapshotHighestSegmentIndex, boundaryFile.BoundaryIndex())

		// The lower bound file we previously wrote should still be present.
		lowerBoundFile, err := disktable.LoadBoundaryFile(disktable.LowerBound, path.Join(snapshotDir, tableName))
		require.NoError(t, err)
		require.True(t, lowerBoundFile.IsDefined())
		require.Equal(t, lowerBoundsByTable[tableName], lowerBoundFile.BoundaryIndex())

		for i := snapshotLowestSegmentIndex; i <= snapshotHighestSegmentIndex; i++ {
			regularSegment := segments[i]
			snapshotSegment := snapshotSegments[i]

			// The regular segment should know it is not a snapshot.
			snapshot, err := regularSegment.IsSnapshot()
			require.NoError(t, err)
			require.False(t, snapshot)

			// None of the regular segment files should be symlinks.
			for _, filePath := range regularSegment.GetFilePaths() {
				info, err := os.Lstat(filePath)
				require.NoError(t, err)
				require.False(t, info.Mode()&os.ModeSymlink != 0)
			}

			// The snapshot segment should realize that it is a snapshot.
			snapshot, err = snapshotSegment.IsSnapshot()
			require.NoError(t, err)
			require.True(t, snapshot)

			// All snapshot files should be symlinks.
			for _, filePath := range snapshotSegment.GetFilePaths() {
				info, err := os.Lstat(filePath)
				require.NoError(t, err)
				require.True(t, info.Mode()&os.ModeSymlink != 0)
			}

			// The keys should be the same in both segments.
			regularKeys, err := regularSegment.GetKeys()
			require.NoError(t, err)
			snapshotKeys, err := snapshotSegment.GetKeys()
			require.NoError(t, err)
			require.Equal(t, regularKeys, snapshotKeys)

			// The values should be present in both segments.
			for _, key := range regularKeys {
				regularValue, err := regularSegment.Read(key.Key, key.Address)
				require.NoError(t, err)

				snapshotValue, err := snapshotSegment.Read(key.Key, key.Address)
				require.NoError(t, err)

				require.Equal(t, regularValue, snapshotValue)
			}
		}
	}

	// Cleanup.
	err = db.Close()
	require.NoError(t, err)

	ok, err = errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)
}
