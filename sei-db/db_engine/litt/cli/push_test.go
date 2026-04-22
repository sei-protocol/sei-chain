package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func pushTest(
	t *testing.T,
	sourceDirs uint64,
	destDirs uint64,
	verbose bool,
) {
	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDir := t.TempDir()
	sourceRoot := path.Join(testDir, "source")
	destRoot := path.Join(testDir, "dest")

	err := os.MkdirAll(sourceRoot, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(destRoot, 0755)
	require.NoError(t, err)

	// Start a container that is running an SSH server. The push() command will communicate with this server.
	container := util.SetupSSHTestContainer(t, destRoot)
	defer container.Cleanup()

	sourceDirList := make([]string, 0, sourceDirs)
	// The destination directories relative to the test's perspective of the filesystem.
	destDirList := make([]string, 0, destDirs)
	// The destination directories relative to the container's perspective of the filesystem.
	dockerDestDirList := make([]string, 0, destDirs)

	for i := uint64(0); i < sourceDirs; i++ {
		sourceDirList = append(sourceDirList, path.Join(sourceRoot, fmt.Sprintf("source-%d", i)))
	}
	for i := uint64(0); i < destDirs; i++ {
		dir := fmt.Sprintf("dest-%d", i)
		destDirList = append(destDirList, path.Join(destRoot, dir))
		dockerDestDirList = append(dockerDestDirList, path.Join(container.GetDataDir(), dir))
	}

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
	config.TargetSegmentFileSize = 1024

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
		// We should not see dest dirs yet.
		exists, err := util.Exists(destDir)
		require.NoError(t, err)
		require.False(t, exists, "destination directory %s exists", destDir)
	}

	// pushing with the DB still open should fail.
	err = push(logger, sourceDirList, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", false,
		false, 2, 1, verbose)
	require.Error(t, err)

	// None of the source dirs should have been deleted.
	for _, sourceDir := range sourceDirList {
		// We should see each source dir.
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)
		require.True(t, exists, "source directory %s does not exist", sourceDir)
	}

	// The failed push should not have changed the data in the DB.
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

	//// Shut down the DB and push it.
	err = db.Close()
	require.NoError(t, err, "failed to close DB")

	// Deleting after transfer is only support for snapshots (which we are not testing here).
	err = push(logger, sourceDirList, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", true,
		false, 2, 1, verbose)
	require.Error(t, err)

	// Actually push it correctly now.
	err = push(logger, sourceDirList, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", false,
		false, 8, 1, verbose)
	require.NoError(t, err, "failed to close DB")

	// Verify the new directories.
	for _, sourceDir := range sourceDirList {
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)

		// Even if we are deleting after transfer, the source directories should still exist.
		require.True(t, exists, "source directory %s does not exist but should", sourceDir)
	}
	for _, destDir := range destDirList {
		// We should see all destination dirs.
		exists, err := util.Exists(destDir)
		require.NoError(t, err)
		require.True(t, exists, "destination directory %s does not exist", destDir)
	}

	// Push works when there is nothing at the destination. It also works when some of the files are present or
	// corrupted. Let's mess with the files at the destination and make sure that the push command is able to fix
	// things afterward.
	filesInTree := make([]string, 0)
	err = filepath.Walk(destRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip directories.
			return nil
		}

		filesInTree = append(filesInTree, path)

		return nil
	})
	require.NoError(t, err)

	for _, segmentFile := range filesInTree {
		choice := rand.Float64()

		if choice < 0.3 {
			// Delete the file. Push will copy it over again.
			err = os.Remove(segmentFile)
			require.NoError(t, err, "failed to delete file %s", segmentFile)
		} else if choice < 0.6 {
			// Overwrite the file with random data. Push will replace it with the correct data.
			randomData := rand.Bytes(128)
			// use broad file permissions to avoid issues with container user having different UID/GID.
			err = os.WriteFile(segmentFile, randomData, 0666)
			require.NoError(t, err, "failed to overwrite file %s", segmentFile)
		} else if choice < 0.9 {
			// Attempt to move the file to another legal location.

			if len(destDirList) == 1 {
				// We can't move a file to a different directory if there is only one destination directory.
				continue
			}

			// Segment files will have the following format: destRoot/dest-N/tableName/segments/segmentFileName
			//  We want to change the "dest-N" part. This is a legal location for the data, since it doesn't matter
			// which destination directory the data is in, as long as it is in one of them.

			parts := strings.Split(segmentFile, string(os.PathSeparator))
			require.Greater(t, len(parts), 3, "unexpected path format: %s", segmentFile)

			oldDir := parts[len(parts)-4] // This is the "dest-N" part.
			oldDirIndexString := strings.Replace(oldDir, "dest-", "", 1)
			oldDirIndex, err := strconv.Atoi(oldDirIndexString)
			require.NoError(t, err)
			newDirIndex := (oldDirIndex + 1) % len(destDirList) // Move to the next destination directory.
			newPath := strings.Replace(segmentFile, oldDir, fmt.Sprintf("dest-%d", newDirIndex), 1)

			err = os.Rename(segmentFile, newPath)
			require.NoError(t, err)
		}
	}

	// Push again, should fix the messed up files.
	err = push(logger, sourceDirList, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", false,
		false, 2, 1, verbose)
	require.NoError(t, err)

	// Reopen the old DB, verify no data is missing.
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

	// Fully delete the old DB. The new DB should be a copy of the old one, so this should not affect copied data.
	err = db.Destroy()
	require.NoError(t, err)

	// Push should NOT copy the keymap. Verify that there is no keymap directory in destRoot.
	err = filepath.Walk(destRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		require.False(t, strings.Contains(path, keymap.KeymapDirectoryName))
		return nil
	})
	require.NoError(t, err)

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

func TestPush1to1(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	sourceDirs := uint64(1)
	destDirs := uint64(1)

	pushTest(t, sourceDirs, destDirs, false)
}

func TestPush1toN(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	sourceDirs := uint64(1)
	destDirs := uint64(4)

	pushTest(t, sourceDirs, destDirs, false)
}

func TestPushNto1(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	sourceDirs := uint64(4)
	destDirs := uint64(1)

	pushTest(t, sourceDirs, destDirs, false)
}

func TestPushNtoN(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	sourceDirs := uint64(4)
	destDirs := uint64(4)

	// This test is run in verbose mode to make sure we don't crash when that is enabled.
	// Other tests in this file are not run in verbose mode to reduce log clutter.
	pushTest(t, sourceDirs, destDirs, true)
}

func TestPushSnapshot(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	ctx := t.Context()
	logger := test.GetLogger()

	rand := random.NewTestRandom()
	sourceRoot := t.TempDir()
	destRoot := t.TempDir()
	snapshotDir := path.Join(t.TempDir(), "snapshot")

	sourceDirs := rand.Uint64Range(2, 4)
	destDirs := rand.Uint64Range(2, 4)

	// Start a container that is running an SSH server. The push() command will communicate with this server.
	container := util.SetupSSHTestContainer(t, destRoot)
	defer container.Cleanup()

	sourceDirList := make([]string, 0, sourceDirs)
	// The destination directories relative to the test's perspective of the filesystem.
	destDirList := make([]string, 0, destDirs)
	// The destination directories relative to the container's perspective of the filesystem.
	dockerDestDirList := make([]string, 0, destDirs)

	for i := uint64(0); i < sourceDirs; i++ {
		sourceDirList = append(sourceDirList, path.Join(sourceRoot, fmt.Sprintf("source-%d", i)))
	}
	for i := uint64(0); i < destDirs; i++ {
		dir := fmt.Sprintf("dest-%d", i)
		destDirList = append(destDirList, path.Join(destRoot, dir))
		dockerDestDirList = append(dockerDestDirList, path.Join(container.GetDataDir(), dir))
	}

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
	config.TargetSegmentFileSize = 1024
	config.SnapshotDirectory = snapshotDir

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
		// We should not see dest dirs yet.
		exists, err := util.Exists(destDir)
		require.NoError(t, err)
		require.False(t, exists, "destination directory %s exists", destDir)
	}

	// pushing with the DB still open should fail.
	err = push(logger, sourceDirList, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", false,
		false, 2, 1, false)
	require.Error(t, err)

	// None of the source dirs should have been deleted.
	for _, sourceDir := range sourceDirList {
		// We should see each source dir.
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)
		require.True(t, exists, "source directory %s does not exist", sourceDir)
	}

	// The failed push should not have changed the data in the DB.
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

	// Power cycle the DB twice. After the first shutdown, the last segment with data will not have been copied
	// to the snapshot directory. When the database starts a second time, it will seal the last segment and make
	// sure the snapshot directory includes it.
	err = db.Close()
	require.NoError(t, err, "failed to close DB")

	// Find the highest segment index for each table. We will use it to do verification later.
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	highestSegmentIndexForTable := make(map[string]uint32)
	for tableName := range expectedData {
		segmentPaths, err := segment.BuildSegmentPaths(sourceDirList, "", tableName)
		require.NoError(t, err, "failed to build segment paths for table %s", tableName)
		_, highestSegmentIndex, _, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			segmentPaths,
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)
		highestSegmentIndexForTable[tableName] = highestSegmentIndex
	}
	ok, err := errorMonitor.IsOk()
	require.NoError(t, err)
	require.True(t, ok)

	// Second power cycle
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "failed to get table %s", tableName)
		err = table.Flush()
		require.NoError(t, err, "failed to flush table %s", table.Name())
	}
	err = db.Close()
	require.NoError(t, err, "failed to close DB after second open")

	// Push the data. Do not delete the snapshot yet.
	err = push(logger, []string{snapshotDir}, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", false,
		false, 8, 1, false)
	require.NoError(t, err, "failed to close DB")

	// Verify the new directories.
	for _, sourceDir := range sourceDirList {
		exists, err := util.Exists(sourceDir)
		require.NoError(t, err)

		// Even if we are deleting after transfer, the source directories should still exist.
		require.True(t, exists, "source directory %s does not exist but should", sourceDir)
	}
	for _, destDir := range destDirList {
		// We should see all destination dirs.
		exists, err := util.Exists(destDir)
		require.NoError(t, err)
		require.True(t, exists, "destination directory %s does not exist", destDir)
	}

	// Push works when there is nothing at the destination. It also works when some of the files are present or
	// corrupted. Let's mess with the files at the destination and make sure that the push command is able to fix
	// things afterward.
	filesInTree := make([]string, 0)
	err = filepath.Walk(destRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip directories.
			return nil
		}

		filesInTree = append(filesInTree, path)

		return nil
	})
	require.NoError(t, err)

	for _, segmentFile := range filesInTree {
		choice := rand.Float64()

		if choice < 0.3 {
			// Delete the file. Push will copy it over again.
			err = os.Remove(segmentFile)
			require.NoError(t, err, "failed to delete file %s", segmentFile)
		} else if choice < 0.6 {
			// Overwrite the file with random data. Push will replace it with the correct data.
			randomData := rand.Bytes(128)
			err = os.WriteFile(segmentFile, randomData, 0644)
			require.NoError(t, err, "failed to overwrite file %s", segmentFile)
		} else if choice < 0.9 {
			// Attempt to move the file to another legal location.

			if len(destDirList) == 1 {
				// We can't move a file to a different directory if there is only one destination directory.
				continue
			}

			// Segment files will have the following format: destRoot/dest-N/tableName/segments/segmentFileName
			//  We want to change the "dest-N" part. This is a legal location for the data, since it doesn't matter
			// which destination directory the data is in, as long as it is in one of them.

			parts := strings.Split(segmentFile, string(os.PathSeparator))
			require.Greater(t, len(parts), 3, "unexpected path format: %s", segmentFile)

			oldDir := parts[len(parts)-4] // This is the "dest-N" part.
			oldDirIndexString := strings.Replace(oldDir, "dest-", "", 1)
			oldDirIndex, err := strconv.Atoi(oldDirIndexString)
			require.NoError(t, err)
			newDirIndex := (oldDirIndex + 1) % len(destDirList) // Move to the next destination directory.
			newPath := strings.Replace(segmentFile, oldDir, fmt.Sprintf("dest-%d", newDirIndex), 1)

			err = os.Rename(segmentFile, newPath)
			require.NoError(t, err)
		}
	}

	// Push again, should fix the messed up files. This time, tell the push command to clean up after itself.
	err = push(logger, []string{snapshotDir}, dockerDestDirList, container.GetUser(), container.GetHost(),
		container.GetSSHPort(), container.GetPrivateKeyPath(), "", true,
		false, 2, 1, false)
	require.NoError(t, err)

	// We instructed push() to delete files after pushing. For each table, we should observe a "lower bound" file
	// with a segment index that matches the expected highest segment index for that table. This boundary file signals
	// to LittDB that it shouldn't recreate the snapshot files that have been copied and deleted by push().
	for tableName, highestSegmentIndex := range highestSegmentIndexForTable {
		tableSnapshotDir := path.Join(snapshotDir, tableName)
		boundaryFile, err := disktable.LoadBoundaryFile(false, tableSnapshotDir)
		require.NoError(t, err)
		require.True(t, boundaryFile.IsDefined(), "boundary file for table %s is not defined", tableName)
		require.Equal(t, highestSegmentIndex, boundaryFile.BoundaryIndex())
	}

	// There should be no segment files remaining in the snapshot directory.
	err = filepath.Walk(snapshotDir, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		require.False(t, strings.Contains(path, segment.MetadataFileExtension),
			"unexpected file: %s", path)
		require.False(t, strings.Contains(path, segment.KeyFileExtension),
			"unexpected file: %s", path)
		require.False(t, strings.Contains(path, segment.ValuesFileExtension),
			"unexpected file: %s", path)
		return nil
	})
	require.NoError(t, err)

	// There should also not be any segment files in the hard link directories.
	err = filepath.Walk(sourceRoot, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)

		inHardLinkDir := strings.Contains(path, segment.HardLinkDirectory)
		if !inHardLinkDir {
			return nil
		}

		require.False(t, strings.Contains(path, segment.MetadataFileExtension),
			"unexpected file: %s", path)
		require.False(t, strings.Contains(path, segment.KeyFileExtension),
			"unexpected file: %s", path)
		require.False(t, strings.Contains(path, segment.ValuesFileExtension),
			"unexpected file: %s", path)
		return nil
	})
	require.NoError(t, err)

	// Reopen the old DB, verify no data is missing.
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

	// Fully delete the old DB. The new DB should be a copy of the old one, so this should not affect copied data.
	err = db.Destroy()
	require.NoError(t, err)

	// Push should NOT copy the keymap. Verify that there is no keymap directory in destRoot.
	err = filepath.Walk(destRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		require.False(t, strings.Contains(path, keymap.KeymapDirectoryName))
		return nil
	})
	require.NoError(t, err)

	// Reopen the DB at the new destination directories.
	config.Paths = destDirList
	config.SnapshotDirectory = ""
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
