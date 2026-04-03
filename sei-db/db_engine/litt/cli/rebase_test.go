package main

import (
	"path"
	"testing"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func rebaseTest(
	t *testing.T,
	sourceDirs uint64,
	destDirs uint64,
	overlap uint64,
	preserveOriginal bool,
	verbose bool,
) {
	t.Helper()
	logger := test.GetLogger()

	if overlap > 0 && preserveOriginal {
		require.Fail(t, "Invalid test configuration, cannot preserve original when there is overlap")
	}

	rand := random.NewTestRandom()
	testDir := t.TempDir()

	sourceDirList := make([]string, 0, sourceDirs)
	sourceDirSet := make(map[string]struct{}, sourceDirs)
	destDirList := make([]string, 0, destDirs)
	destDirSet := make(map[string]struct{}, destDirs)

	for i := uint64(0); i < sourceDirs; i++ {
		sourceDir := path.Join(testDir, rand.String(32))
		sourceDirList = append(sourceDirList, path.Join(testDir, sourceDir))
		sourceDirSet[sourceDir] = struct{}{}

		if i < overlap {
			// Reuse this directory for the destination as well.
			destDirList = append(destDirList, sourceDir)
			destDirSet[sourceDir] = struct{}{}
		}
	}
	for len(destDirList) < int(destDirs) {
		destDir := path.Join(testDir, rand.String(32))
		destDirList = append(destDirList, destDir)
		destDirSet[destDir] = struct{}{}
	}

	// Randomize the order of the source and destination directories. This ensures that the first directories
	// are not always the ones that overlap.
	rand.Shuffle(len(sourceDirList), func(i, j int) {
		sourceDirList[i], sourceDirList[j] = sourceDirList[j], sourceDirList[i]
	})
	rand.Shuffle(len(destDirList), func(i, j int) {
		destDirList[i], destDirList[j] = destDirList[j], destDirList[i]
	})

	tableCount := rand.Uint64Range(2, 4)
	tableNames := make([]string, 0, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableNames = append(tableNames, rand.String(32))
	}

	shardingFactor := sourceDirs + rand.Uint64Range(0, 4)

	config, err := litt.DefaultConfig(sourceDirList...)
	require.NoError(t, err)
	config.DoubleWriteProtection = true
	config.ShardingFactor = uint32(shardingFactor)
	config.Fsync = false
	config.TargetSegmentFileSize = 100

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	expectedData := make(map[string] /*table*/ map[string] /*value*/ []byte)
	for _, tableName := range tableNames {
		expectedData[tableName] = make(map[string][]byte)
	}

	// Insert data into the tables.
	keyCount := uint64(1024)
	for i := uint64(0); i < keyCount; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		table, err := db.GetTable(tableNames[tableIndex])
		require.NoError(t, err)
		key := rand.PrintableBytes(32)
		value := rand.PrintableVariableBytes(10, 100)

		expectedData[table.Name()][string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err, "failed to put key %s in table %s", key, table.Name())
	}

	// Flush all tables.
	for _, tableName := range tableNames {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		err = table.Flush()
		require.NoError(t, err, "failed to flush table %s", table.Name())
	}

	// Verify the data in the DB.
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "failed to get table %s", tableName)
		for key := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "failed to get key %s in table %s", key, tableName)
			require.True(t, ok, "key %s not found in table %s", key, tableName)
			require.Equal(t, expectedData[tableName][key], value,
				"value for key %s in table %s does not match expected value", key, tableName)
		}
	}

	// Verify expected directories.
	for _, sourceDir := range sourceDirList {
		// We should see each source dir.
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)
		require.True(t, exists, "source directory %s does not exist", sourceDir)
	}
	for _, destDir := range destDirList {
		// We should not see dest dirs unless they overlap with source dirs.
		exists, err := util.Exists(destDir)
		require.NoError(t, err)
		if _, ok := sourceDirSet[destDir]; !ok {
			require.True(t, !exists, "destination directory %s does not exist", destDir)
		} else {
			require.False(t, exists, "destination directory %s exists", destDir)
		}
	}

	// Rebasing with the DB still open should fail.
	err = rebase(logger, sourceDirList, destDirList, preserveOriginal, false, verbose)
	require.Error(t, err)

	// None of the source dirs should have been deleted.
	for _, sourceDir := range sourceDirList {
		// We should see each source dir.
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)
		require.True(t, exists, "source directory %s does not exist", sourceDir)
	}

	// The failed rebase should not have changed the data in the DB.
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "failed to get table %s", tableName)
		for key := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "failed to get key %s in table %s", key, tableName)
			require.True(t, ok, "key %s not found in table %s", key, tableName)
			require.Equal(t, expectedData[tableName][key], value,
				"value for key %s in table %s does not match expected value", key, tableName)
		}
	}

	// Shut down the DB and rebase it.
	err = db.Close()
	require.NoError(t, err, "failed to close DB")

	err = rebase(logger, sourceDirList, destDirList, preserveOriginal, false, verbose)
	require.NoError(t, err, "failed to rebase DB")

	// Verify the new directories.
	for _, sourceDir := range sourceDirList {
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)

		if preserveOriginal {
			// We should see each source dir if preserveOriginal is true.
			require.True(t, exists, "source directory %s does not exist", sourceDir)
		} else {
			// If we aren't preserving the original, then a source directory should only exist if it overlaps.
			if _, ok := destDirSet[sourceDir]; !ok {
				require.False(t, exists, "source directory %s exists but should not", sourceDir)
			} else {
				require.True(t, exists, "source directory %s does not exist but should", sourceDir)
			}
		}
	}
	for _, destDir := range destDirList {
		// We should see all destination dirs.
		exists, err := util.Exists(destDir)
		require.NoError(t, err)
		require.True(t, exists, "destination directory %s does not exist", destDir)
	}

	// Reopen the DB at the new destination directories.
	config.Paths = destDirList
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err, "failed to open DB after rebase")

	// Verify the data in the DB.
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "failed to get table %s", tableName)
		for key := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "failed to get key %s in table %s", key, tableName)
			require.True(t, ok, "key %s not found in table %s", key, tableName)
			require.Equal(t, expectedData[tableName][key], value,
				"value for key %s in table %s does not match expected value", key, tableName)
		}
	}

	err = db.Close()
	require.NoError(t, err, "failed to close DB after rebase")
}

func TestRebase1to1(t *testing.T) {
	t.Parallel()

	sourceDirs := uint64(1)
	destDirs := uint64(1)

	t.Run("preserve", func(t *testing.T) {
		// This is the only test that runs with verbose= true. We want to make sure this doesn't crash,
		// but don't want too much spam in the logs.
		rebaseTest(t, sourceDirs, destDirs, 0, true, true)
	})

	t.Run("do not preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, false, false)
	})
}

func TestRebase1toN(t *testing.T) {
	t.Parallel()

	sourceDirs := uint64(1)
	destDirs := uint64(4)

	t.Run("preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, true, false)
	})

	t.Run("do not preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, false, false)
	})
}

func TestRebaseNto1(t *testing.T) {
	t.Parallel()

	sourceDirs := uint64(4)
	destDirs := uint64(1)

	t.Run("preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, true, false)
	})

	t.Run("do not preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, false, false)
	})
}

func TestRebaseNtoN(t *testing.T) {
	t.Parallel()

	sourceDirs := uint64(4)
	destDirs := uint64(4)

	t.Run("preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, true, false)
	})

	t.Run("do not preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, false, false)
	})
}

func TestRebaseNtoNOverlap(t *testing.T) {
	t.Parallel()

	sourceDirs := uint64(4)
	destDirs := uint64(4)

	t.Run("preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, true, false)
	})

	t.Run("do not preserve", func(t *testing.T) {
		rebaseTest(t, sourceDirs, destDirs, 0, false, false)
	})
}

// Verify the behavior when we attempt to rebase a snapshot directory.
func TestRebaseSnapshot(t *testing.T) {
	t.Parallel()

	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDir := t.TempDir()

	tableCount := rand.Uint64Range(2, 4)
	tableNames := make([]string, 0, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableNames = append(tableNames, rand.String(32))
	}

	shardingFactor := rand.Uint32Range(1, 4)
	roots := make([]string, 0, shardingFactor)
	for i := uint32(0); i < shardingFactor; i++ {
		roots = append(roots, path.Join(testDir, rand.String(32)))
	}

	snapshotDir := path.Join(testDir, "snapshot")

	config, err := litt.DefaultConfig(roots...)
	require.NoError(t, err)
	config.DoubleWriteProtection = true
	config.ShardingFactor = shardingFactor
	config.Fsync = false
	config.SnapshotDirectory = snapshotDir
	config.TargetSegmentFileSize = 100

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	expectedData := make(map[string] /*table*/ map[string] /*value*/ []byte)
	for _, tableName := range tableNames {
		expectedData[tableName] = make(map[string][]byte)
	}

	// Insert data into the tables.
	keyCount := uint64(1024)
	for i := uint64(0); i < keyCount; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		table, err := db.GetTable(tableNames[tableIndex])
		require.NoError(t, err)
		key := rand.PrintableBytes(32)
		value := rand.PrintableVariableBytes(10, 100)

		expectedData[table.Name()][string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err, "failed to put key %s in table %s", key, table.Name())
	}

	// Flush all tables.
	for _, tableName := range tableNames {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		err = table.Flush()
		require.NoError(t, err, "failed to flush table %s", table.Name())
	}

	// Verify the data in the DB.
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "failed to get table %s", tableName)
		for key := range expectedData[tableName] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "failed to get key %s in table %s", key, tableName)
			require.True(t, ok, "key %s not found in table %s", key, tableName)
			require.Equal(t, expectedData[tableName][key], value,
				"value for key %s in table %s does not match expected value", key, tableName)
		}
	}

	destinationDir := path.Join(testDir, "destination")

	// Begin the rebase without shutting down the DB. Lock files on the snapshot directory shouldn't interfere,
	// but we still expect it to fail, since we don't support rebasing a snapshot directory.
	err = rebase(
		logger,
		[]string{snapshotDir},
		[]string{destinationDir},
		true,
		false,
		false)
	require.Error(t, err)

	err = db.Close()
	require.NoError(t, err, "failed to close DB after rebase")

	// It won't matter that the DB is closed, we still expect the rebase to fail.
	err = rebase(
		logger,
		[]string{snapshotDir},
		[]string{destinationDir},
		true,
		false,
		false)
	require.Error(t, err)
}
