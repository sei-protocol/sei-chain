package test

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	testrandom "github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

// Note: this test is defined in the test package to avoid circular dependencies.

func TestUnlock(t *testing.T) {
	testDir := t.TempDir()
	rand := testrandom.NewTestRandom()
	volumes := []string{path.Join(testDir, "volume1"), path.Join(testDir, "volume2"), path.Join(testDir, "volume3")}

	config, err := litt.DefaultConfig(volumes...)
	config.Fsync = false // Disable fsync for faster tests
	config.TargetSegmentFileSize = 100
	config.ShardingFactor = uint32(len(volumes))
	require.NoError(t, err)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	table, err := db.GetTable("test_table")
	require.NoError(t, err)

	expectedData := make(map[string][]byte)

	// Write some data
	for i := 0; i < 100; i++ {
		key := rand.PrintableBytes(32)
		value := rand.PrintableVariableBytes(1, 100)

		expectedData[string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err, "Failed to put data in table")
	}

	// Look for lock files. We should see one for each volume.
	lockFileCount := 0
	err = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but do not fail. LittDB may be shuffling files around concurrently.
			t.Logf("Error walking path %s (not necessarily fatal): %v", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, util.LockfileName) {
			lockFileCount++
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, lockFileCount)

	// Unlock the DB. This should remove all lock files, but leave other files intact.
	err = disktable.Unlock(config.Logger, volumes)
	require.NoError(t, err, "Failed to unlock the database")

	// There should be no lock files left.
	lockFileCount = 0
	err = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but do not fail. LittDB may be shuffling files around concurrently.
			t.Logf("Error walking path %s (not necessarily fatal): %v", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, util.LockfileName) {
			lockFileCount++
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 0, lockFileCount, "There should be no lock files left after unlocking")

	// Calling unlock again should not cause any issues.
	err = disktable.Unlock(config.Logger, volumes)
	require.NoError(t, err, "Failed to unlock the database again")

	// Verify that the data is still intact.
	for key, expectedValue := range expectedData {
		value, ok, err := table.Get([]byte(key))
		require.NoError(t, err, "Failed to get data from table")
		require.True(t, ok, "Failed to get data from table")
		require.Equal(t, expectedValue, value, "Data mismatch for key %s", key)
	}

	// Restart the database and verify the data again.
	err = db.Close()
	require.NoError(t, err)

	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)

	table, err = db.GetTable("test_table")
	require.NoError(t, err)

	for key, expectedValue := range expectedData {
		value, ok, err := table.Get([]byte(key))
		require.NoError(t, err, "Failed to get data from table after restart")
		require.True(t, ok, "Failed to get data from table after restart")
		require.Equal(t, expectedValue, value, "Data mismatch for key %s after restart", key)
	}

	err = db.Close()
	require.NoError(t, err, "Failed to close the database after restart")
}

func TestPurgeLocks(t *testing.T) {
	testDir := t.TempDir()
	rand := testrandom.NewTestRandom()
	volumes := []string{path.Join(testDir, "volume1", path.Join(testDir, "volume2"), path.Join(testDir, "volume3"))}

	config, err := litt.DefaultConfig(volumes...)
	config.Fsync = false // Disable fsync for faster tests
	config.TargetSegmentFileSize = 100
	require.NoError(t, err)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	table, err := db.GetTable("test_table")
	require.NoError(t, err)

	expectedData := make(map[string][]byte)

	// Write some data
	for i := 0; i < 100; i++ {
		key := rand.PrintableBytes(32)
		value := rand.PrintableVariableBytes(1, 100)

		expectedData[string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err, "Failed to put data in table")
	}

	// Opening a second instance of the database should fail due to existing locks.
	_, err = littbuilder.NewDB(config)
	require.Error(t, err, "Expected error when opening a second instance of the database with existing locks")

	// Open a new instance of the database at the same time. Normally this is not possible, but it becomes possible
	// when we purge locks.
	config.PurgeLocks = true
	db2, err := littbuilder.NewDB(config)
	require.NoError(t, err, "Failed to open a second instance of the database")

	// This test doesn't bother to verify the table data, since we are in unsafe territory now with multiple instances
	// of the database running at the same time.

	err = db.Close()
	require.NoError(t, err, "Failed to close the first instance of the database")
	err = db2.Close()
	require.NoError(t, err)
}
