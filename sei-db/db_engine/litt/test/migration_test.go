package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Layr-Labs/eigenda/core"
	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

// This file contains tests for data migrations (i.e. when the on-disk format of the data changes).

// Enable and run this "test" to generate data for a migration test at the current version.
func TestGenerateData(t *testing.T) {
	t.Skip() // comment out this line to generate data

	version := segment.LatestSegmentVersion
	dataDir := fmt.Sprintf("testdata/v%d", version)

	exists, err := util.Exists(dataDir)
	require.NoError(t, err)
	if exists {
		fmt.Printf("deleting existing data at %s\n", dataDir)
		err = os.RemoveAll(dataDir)
		require.NoError(t, err)
	}

	fmt.Printf("generating migration test data at %s\n", dataDir)

	err = os.MkdirAll(dataDir, 0777)
	require.NoError(t, err)

	config, err := litt.DefaultConfig(dataDir)
	require.NoError(t, err)
	config.DoubleWriteProtection = true
	config.Fsync = false
	config.ShardingFactor = 4
	config.TargetSegmentFileSize = 100

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	table, err := db.GetTable("test")
	require.NoError(t, err)

	for key, value := range migrationData {
		err = table.Put([]byte(key), []byte(value))
		require.NoError(t, err)
	}

	// verify the data in the table
	for key, value := range migrationData {
		v, exists, err := table.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, value, string(v))
	}

	// Shut the DB down.
	err = db.Close()
	require.NoError(t, err)
}

func TestMigration(t *testing.T) {

	// Find all copies of the table at various versions. We will run a migration test on each of them.
	migrationPaths := make([]string, 0)

	// Get direct subdirectories of "testdata/" - only these contain version data
	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() {
			versionDir := filepath.Join("testdata", entry.Name())
			// Only include directories with 'v' prefix (version directories)
			if len(entry.Name()) > 0 && entry.Name()[0] == 'v' {
				migrationPaths = append(migrationPaths, versionDir)
			}
		}
	}

	// Skip the test if no version directories are found
	require.NotEmpty(t, migrationPaths, "No version directories found in testdata/")

	currentVersion := segment.LatestSegmentVersion
	for _, migrationPath := range migrationPaths {

		// Each migration path is in the format "v[version]".
		oldVersion, err := strconv.Atoi(filepath.Base(migrationPath)[1:])
		require.NoError(t, err)

		t.Run(fmt.Sprintf("%d->%d", oldVersion, currentVersion), func(t *testing.T) {
			testMigration(t, migrationPath)
		})
	}

}

func testMigration(t *testing.T, migrationPath string) {
	rand := random.NewTestRandom()

	// Make a copy of the data so we don't modify the original (which is checked into git).
	testDir := t.TempDir()

	err := os.MkdirAll(testDir, 0777)
	require.NoError(t, err)

	// Copy the test data directory to our temporary directory
	err = util.RecursiveMove(migrationPath, testDir, true, false)
	require.NoError(t, err)

	// Now open the database and verify the data matches our expectations
	config, err := litt.DefaultConfig(testDir)
	require.NoError(t, err)
	config.DoubleWriteProtection = true
	config.Fsync = false

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	t.Cleanup(func() { core.CloseLogOnError(db, "littdb", nil) })

	table, err := db.GetTable("test")
	require.NoError(t, err)

	// Verify the data in the table matches our expected data
	for key, value := range migrationData {
		v, exists, err := table.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, value, string(v))
	}

	// Write some new data to the table to ensure we can read and write after migration
	newData := make(map[string]string)
	const numNewItems = 50
	for i := 0; i < numNewItems; i++ {
		key := fmt.Sprintf("newkey-%d-%s", i, rand.PrintableBytes(32))
		value := rand.PrintableBytes(32)
		newData[key] = string(value)

		err := table.Put([]byte(key), value)
		require.NoError(t, err, "Failed to write new data after migration")
	}

	// Verify all the new data can be read back correctly
	for key, value := range newData {
		v, exists, err := table.Get([]byte(key))
		require.NoError(t, err, "Error reading back new data")
		require.True(t, exists, "New data doesn't exist")
		require.Equal(t, value, string(v), "New data doesn't match")
	}

	// Verify the original data.
	for key, value := range migrationData {
		v, exists, err := table.Get([]byte(key))
		require.NoError(t, err, "Error reading migration data")
		require.True(t, exists, "Migration data doesn't exist")
		require.Equal(t, value, string(v), "Migration data doesn't match")
	}

	// Close and reopen the database to ensure persistence
	err = db.Close()
	require.NoError(t, err, "Failed to close database")

	// Reopen the database
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err, "Failed to reopen database")

	table, err = db.GetTable("test")
	require.NoError(t, err, "Failed to get table after reopening")

	// Verify original migration data is still intact
	for key, value := range migrationData {
		v, exists, err := table.Get([]byte(key))
		require.NoError(t, err, "Error reading migration data after reopen")
		require.True(t, exists, "Migration data doesn't exist after reopen")
		require.Equal(t, value, string(v), "Migration data doesn't match after reopen")
	}

	// Verify the new data is still intact
	for key, value := range newData {
		v, exists, err := table.Get([]byte(key))
		require.NoError(t, err, "Error reading new data after reopen")
		require.True(t, exists, "New data doesn't exist after reopen")
		require.Equal(t, value, string(v), "New data doesn't match after reopen")
	}

	err = db.Destroy()
	require.NoError(t, err, "Failed to destroy database")
}
