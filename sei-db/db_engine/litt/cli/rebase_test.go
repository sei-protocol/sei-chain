package main

import (
	"fmt"
	"log/slog"
	"path"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// buildTables builds the named tables once (BuildTable errors if a table is opened more than once) and returns
// a map of name to handle.
func buildTables(t *testing.T, db litt.DB, tableConfig litt.TableConfig, names []string) map[string]litt.Table {
	tables := make(map[string]litt.Table, len(names))
	for _, name := range names {
		cfg := tableConfig
		cfg.Name = name
		table, err := db.BuildTable(cfg)
		require.NoError(t, err)
		tables[name] = table
	}
	return tables
}

func rebaseTest(
	t *testing.T,
	sourceDirs uint64,
	destDirs uint64,
	overlap uint64,
	preserveOriginal bool,
	verbose bool,
) {
	t.Helper()
	logger := slog.Default()

	if overlap > 0 && preserveOriginal {
		require.Fail(t, "Invalid test configuration, cannot preserve original when there is overlap")
	}

	rand := util.NewTestRandom()
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
	config.Fsync = false
	config.TargetSegmentFileSize = 100

	tableConfig := litt.DefaultTableConfig("")
	tableConfig.ShardingFactor = uint8(shardingFactor)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tables := buildTables(t, db, tableConfig, tableNames)

	expectedData := make(map[string] /*table*/ map[string] /*value*/ []byte)
	for _, tableName := range tableNames {
		expectedData[tableName] = make(map[string][]byte)
	}

	// Insert data into the tables.
	keyCount := uint64(1024)
	for i := uint64(0); i < keyCount; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		table := tables[tableNames[tableIndex]]
		key := rand.PrintableBytes(32)
		value := rand.PrintableVariableBytes(10, 100)

		expectedData[table.Name()][string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err, "failed to put key %s in table %s", key, table.Name())
	}

	// Flush all tables.
	for _, tableName := range tableNames {
		table := tables[tableName]
		err = table.Flush()
		require.NoError(t, err, "failed to flush table %s", table.Name())
	}

	// Verify the data in the DB.
	for tableName := range expectedData {
		table := tables[tableName]
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
		table := tables[tableName]
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

	tables = buildTables(t, db, tableConfig, tableNames)

	// Verify the data in the DB.
	for tableName := range expectedData {
		table := tables[tableName]
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

	logger := slog.Default()
	rand := util.NewTestRandom()
	testDir := t.TempDir()

	tableCount := rand.Uint64Range(2, 4)
	tableNames := make([]string, 0, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableNames = append(tableNames, rand.String(32))
	}

	shardingFactor := uint8(rand.Uint32Range(1, 4))
	roots := make([]string, 0, shardingFactor)
	for i := uint8(0); i < shardingFactor; i++ {
		roots = append(roots, path.Join(testDir, rand.String(32)))
	}

	snapshotDir := path.Join(testDir, "snapshot")

	config, err := litt.DefaultConfig(roots...)
	require.NoError(t, err)
	config.DoubleWriteProtection = true
	config.Fsync = false
	config.SnapshotDirectory = snapshotDir
	config.TargetSegmentFileSize = 100

	tableConfig := litt.DefaultTableConfig("")
	tableConfig.ShardingFactor = shardingFactor

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tables := buildTables(t, db, tableConfig, tableNames)

	expectedData := make(map[string] /*table*/ map[string] /*value*/ []byte)
	for _, tableName := range tableNames {
		expectedData[tableName] = make(map[string][]byte)
	}

	// Insert data into the tables.
	keyCount := uint64(1024)
	for i := uint64(0); i < keyCount; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		table := tables[tableNames[tableIndex]]
		key := rand.PrintableBytes(32)
		value := rand.PrintableVariableBytes(10, 100)

		expectedData[table.Name()][string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err, "failed to put key %s in table %s", key, table.Name())
	}

	// Flush all tables.
	for _, tableName := range tableNames {
		table := tables[tableName]
		err = table.Flush()
		require.NoError(t, err, "failed to flush table %s", table.Name())
	}

	// Verify the data in the DB.
	for tableName := range expectedData {
		table := tables[tableName]
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

// TestRebaseMigratesGCWatermark verifies that the rebase CLI moves a table's gc-watermark file along with
// its keymap. Without this, the watermark is orphaned in the source root: it never follows the keymap to its
// new root, and the leftover file makes the final os.Remove(sourceTableDir) fail with "directory not empty".
func TestRebaseMigratesGCWatermark(t *testing.T) {
	logger := slog.Default()
	testDir := t.TempDir()

	sourceDirs := []string{path.Join(testDir, "src0"), path.Join(testDir, "src1")}
	destDirs := []string{path.Join(testDir, "dst0"), path.Join(testDir, "dst1")}

	tableName := "watermarked"

	config, err := litt.DefaultConfig(sourceDirs...)
	require.NoError(t, err)
	config.Fsync = false
	config.TargetSegmentFileSize = 100

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	table, err := db.BuildTable(litt.DefaultTableConfig(tableName))
	require.NoError(t, err)

	for i := 0; i < 200; i++ {
		require.NoError(t, table.Put([]byte(fmt.Sprintf("key-%06d", i)), []byte(fmt.Sprintf("value-%06d", i))))
	}
	require.NoError(t, table.Flush())
	require.NoError(t, db.Close())

	// Locate the keymap's source root and plant a gc-watermark in that table root, as a session that ran GC
	// before being shut down would have left behind.
	keymapDir, _, _, err := littbuilder.FindKeymapLocation(sourceDirs, tableName)
	require.NoError(t, err)
	require.NotEmpty(t, keymapDir)
	sourceTableRoot := path.Dir(keymapDir)

	watermark, err := disktable.LoadGCWatermarkFile(sourceTableRoot)
	require.NoError(t, err)
	require.NoError(t, watermark.Update(3))

	// Rebase from the source roots to the (disjoint) destination roots.
	err = rebase(logger, sourceDirs, destDirs, false, false, false)
	require.NoError(t, err)

	// The source table directory must be gone; the leftover watermark previously blocked its removal.
	exists, err := util.Exists(sourceTableRoot)
	require.NoError(t, err)
	require.False(t, exists, "source table directory should have been removed")

	// The watermark must have followed the keymap to its destination root.
	destKeymapDir, _, _, err := littbuilder.FindKeymapLocation(destDirs, tableName)
	require.NoError(t, err)
	require.NotEmpty(t, destKeymapDir)
	destWatermarkPath := path.Join(path.Dir(destKeymapDir), disktable.GCWatermarkFileName)
	exists, err = util.Exists(destWatermarkPath)
	require.NoError(t, err)
	require.True(t, exists, "gc-watermark should have been migrated to the destination keymap root")
}
