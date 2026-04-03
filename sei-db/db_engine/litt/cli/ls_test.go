package main

import (
	"fmt"
	"sort"
	"testing"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestLs(t *testing.T) {
	t.Parallel()

	logger := test.GetLogger()
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

	// Enable snapshotting.
	snapshotDir := t.TempDir()
	config.SnapshotDirectory = snapshotDir

	// Build the DB and a handful of tables.
	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint32Range(2, 5)
	tables := make([]litt.Table, 0, tableCount)
	expectedData := make(map[string]map[string][]byte)
	tableNames := make([]string, 0, tableCount)
	for i := 0; i < int(tableCount); i++ {
		tableName := fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8))
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables = append(tables, table)
		expectedData[table.Name()] = make(map[string][]byte)
		tableNames = append(tableNames, tableName)
	}

	// Alphabetize table names. ls should always return tables in this order.
	sort.Strings(tableNames)

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

	// We should not be able to call ls on the core directories while the table holds a lock.
	for _, root := range roots {
		_, err = ls(logger, root, true, false)
		require.Error(t, err)
	}
	_, err = lsPaths(logger, roots, true, false)
	require.Error(t, err)

	// Even when the DB is running, it should always be possible to ls the snapshot directory.
	lsResult, err := ls(logger, snapshotDir, true, false)
	require.NoError(t, err)
	require.Equal(t, tableNames, lsResult)

	lsResult, err = lsPaths(logger, []string{snapshotDir}, true, false)
	require.NoError(t, err)
	require.Equal(t, tableNames, lsResult)

	err = db.Close()
	require.NoError(t, err)

	// Now that the DB is closed, we should be able to ls it. We should find all tables defined regardless of which
	// root directory we peer into.
	for _, root := range roots {
		lsResult, err = ls(logger, root, true, false)
		require.NoError(t, err)
		require.Equal(t, tableNames, lsResult)
	}

	lsResult, err = lsPaths(logger, roots, true, true)
	require.NoError(t, err)
	require.Equal(t, tableNames, lsResult)

	// Data should still be present in the snapshot directory.
	lsResult, err = ls(logger, snapshotDir, true, false)
	require.NoError(t, err)
	require.Equal(t, tableNames, lsResult)
}
