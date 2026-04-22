package test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

// Verify that we cannot open a second instance of the database with the same root directories while the
// first instance is running.
func TestDBLocking(t *testing.T) {
	t.Parallel()

	rand := random.NewTestRandom()
	directory := t.TempDir()

	// Spread data across several root directories.
	rootCount := rand.Uint32Range(2, 5)
	roots := make([]string, 0, rootCount)
	for i := 0; i < int(rootCount); i++ {
		roots = append(roots, fmt.Sprintf("%s/root-%d", directory, i))
	}

	config, err := litt.DefaultConfig(roots...)
	require.NoError(t, err)

	// Make it so that we have at least as many shards as roots.
	config.ShardingFactor = rootCount * rand.Uint32Range(1, 4)

	// Settings that should be enabled for LittDB unit tests.
	config.DoubleWriteProtection = true
	config.Fsync = false

	// Use small segments to ensure that we create a few segments per table.
	config.TargetSegmentFileSize = 100

	// Build the DB and a handful of tables.
	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint32Range(2, 5)
	tables := make([]litt.Table, 0, tableCount)
	expectedData := make(map[string]map[string][]byte)
	for i := 0; i < int(tableCount); i++ {
		tableName := fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8))
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables = append(tables, table)
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Insert some data into the tables.
	for _, table := range tables {
		for i := 0; i < 100; i++ {
			key := rand.PrintableBytes(32)
			value := rand.PrintableVariableBytes(10, 200)
			expectedData[table.Name()][string(key)] = value
			err = table.Put(key, value)
			require.NoError(t, err, "Failed to put key-value pair in table %s", table.Name())
		}
		err = table.Flush()
		require.NoError(t, err, "Failed to flush table %s", table.Name())
	}

	// Verify that the data is correctly stored in the tables.
	for _, table := range tables {
		for key, expectedValue := range expectedData[table.Name()] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "Failed to get value for key %s in table %s", key, table.Name())
			require.True(t, ok, "Key %s not found in table %s", key, table.Name())
			require.Equal(t, expectedValue, value,
				"Value mismatch for key %s in table %s", key, table.Name())
		}
	}

	// Attempt to open a second instance of the database with the same root directories. Locking should prevent this.
	shadowConfig, err := litt.DefaultConfig(roots...)
	require.NoError(t, err)
	shadowConfig.ShardingFactor = config.ShardingFactor
	shadowConfig.DoubleWriteProtection = true
	shadowConfig.Fsync = false

	_, err = littbuilder.NewDB(shadowConfig)
	require.Error(t, err,
		"Expected error when opening a second instance of the database with the same root directories")

	// Even sharing just one root should be enough to torpedo the second instance.
	shadowConfig, err = litt.DefaultConfig(roots[:1]...)
	require.NoError(t, err)
	shadowConfig.ShardingFactor = config.ShardingFactor
	shadowConfig.DoubleWriteProtection = true
	shadowConfig.Fsync = false

	_, err = littbuilder.NewDB(shadowConfig)
	require.Error(t, err,
		"Expected error when opening a second instance of the database with the same root directories")

	// Shutting down the database should release the locks.
	err = db.Close()
	require.NoError(t, err, "Failed to close the database")

	// Ensure that we can now open a second instance of the database.
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err, "Failed to open a second instance of the database after closing the first")

	tables = make([]litt.Table, 0, tableCount)
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "Failed to get table %s after reopening the database", tableName)
		tables = append(tables, table)
	}

	// Verify that the data is correctly stored in the tables.
	for _, table := range tables {
		for key, expectedValue := range expectedData[table.Name()] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "Failed to get value for key %s in table %s", key, table.Name())
			require.True(t, ok, "Key %s not found in table %s", key, table.Name())
			require.Equal(t, expectedValue, value,
				"Value mismatch for key %s in table %s", key, table.Name())
		}
	}

	err = db.Destroy()
	require.NoError(t, err, "Failed to destroy the database after testing locking")
}

// If the database process is killed, it may leave behind lock files. Simulate this scenario.
func TestDeadProcessSimulation(t *testing.T) {
	t.Parallel()

	rand := random.NewTestRandom()
	directory := t.TempDir()

	// Spread data across several root directories.
	rootCount := rand.Uint32Range(2, 5)
	roots := make([]string, 0, rootCount)
	for i := 0; i < int(rootCount); i++ {
		roots = append(roots, fmt.Sprintf("%s/root-%d", directory, i))
	}

	config, err := litt.DefaultConfig(roots...)
	require.NoError(t, err)

	// Make it so that we have at least as many shards as roots.
	config.ShardingFactor = rootCount * rand.Uint32Range(1, 4)

	// Settings that should be enabled for LittDB unit tests.
	config.DoubleWriteProtection = true
	config.Fsync = false

	// Use small segments to ensure that we create a few segments per table.
	config.TargetSegmentFileSize = 100

	// Build the DB and a handful of tables.
	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint32Range(2, 5)
	tables := make([]litt.Table, 0, tableCount)
	expectedData := make(map[string]map[string][]byte)
	for i := 0; i < int(tableCount); i++ {
		tableName := fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8))
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables = append(tables, table)
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Insert some data into the tables.
	for _, table := range tables {
		for i := 0; i < 100; i++ {
			key := rand.PrintableBytes(32)
			value := rand.PrintableVariableBytes(10, 200)
			expectedData[table.Name()][string(key)] = value
			err = table.Put(key, value)
			require.NoError(t, err, "Failed to put key-value pair in table %s", table.Name())
		}
		err = table.Flush()
		require.NoError(t, err, "Failed to flush table %s", table.Name())
	}

	// Verify that the data is correctly stored in the tables.
	for _, table := range tables {
		for key, expectedValue := range expectedData[table.Name()] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "Failed to get value for key %s in table %s", key, table.Name())
			require.True(t, ok, "Key %s not found in table %s", key, table.Name())
			require.Equal(t, expectedValue, value,
				"Value mismatch for key %s in table %s", key, table.Name())
		}
	}

	err = db.Close()
	require.NoError(t, err, "Failed to close the database before simulating dead process")

	// Find a PID that does not have an active process.
	pid := int(rand.Int64Range(10000, 20000))
	for util.IsProcessAlive(pid) {
		pid = int(rand.Int64Range(10000, 20000))
	}

	// Write lock files for the simulated dead process.
	for _, root := range roots {
		lockFilePath := fmt.Sprintf("%s/%s", root, util.LockfileName)
		lockFile, err := os.Create(lockFilePath)
		require.NoError(t, err, "Failed to create lock file for simulated dead process at %s", lockFilePath)

		err = WriteLockFile(lockFile, pid)
		require.NoError(t, err, "Failed to write lock file for simulated dead process at %s", lockFilePath)
	}

	// We should still be able to open a new instance of the database since there is no process running with the PID.
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err, "Failed to open a new instance of the database after simulating dead process")

	tables = make([]litt.Table, 0, tableCount)
	for tableName := range expectedData {
		table, err := db.GetTable(tableName)
		require.NoError(t, err, "Failed to get table %s after reopening the database", tableName)
		tables = append(tables, table)
	}

	// Verify that the data is correctly stored in the tables.
	for _, table := range tables {
		for key, expectedValue := range expectedData[table.Name()] {
			value, ok, err := table.Get([]byte(key))
			require.NoError(t, err, "Failed to get value for key %s in table %s", key, table.Name())
			require.True(t, ok, "Key %s not found in table %s", key, table.Name())
			require.Equal(t, expectedValue, value,
				"Value mismatch for key %s in table %s", key, table.Name())
		}
	}

	err = db.Destroy()
	require.NoError(t, err, "Failed to destroy the database after testing locking")
}

// WriteLockFile writes the current process ID and timestamp to the lock file.
func WriteLockFile(lockFile *os.File, pid int) error {
	lockInfo := fmt.Sprintf("PID: %d\nTimestamp: %s\n", pid, time.Now().Format(time.RFC3339))
	_, err := lockFile.WriteString(lockInfo)
	return err
}
