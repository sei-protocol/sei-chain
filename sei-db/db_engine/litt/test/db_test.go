package test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/memtable"
	"github.com/Layr-Labs/eigenda/litt/metrics"
	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/stretchr/testify/require"
)

type dbBuilder struct {
	name    string
	builder func(t *testing.T, tableDirectory string) (litt.DB, error)
}

var builders = []*dbBuilder{
	{
		name:    "mem",
		builder: buildMemDB,
	},
	{
		name:    "mem keymap disk table",
		builder: buildMemKeyDiskDB,
	},
	{
		name:    "levelDB keymap disk table",
		builder: buildLevelDBDiskDB,
	},
}

var restartableBuilders = []*dbBuilder{
	{
		name:    "mem keymap disk table",
		builder: buildMemKeyDiskDB,
	},
	{
		name:    "levelDB keymap disk table",
		builder: buildLevelDBDiskDB,
	},
}

var flushLimitedBuilder = &dbBuilder{
	name:    "levelDB keymap disk table with flush limiter",
	builder: buildLevelDBDiskDBWithFlushLimiter,
}

func buildMemDB(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)

	config.GCPeriod = 50 * time.Millisecond
	config.Logger = test.GetLogger()

	tb := func(
		ctx context.Context,
		logger logging.Logger,
		name string,
		metrics *metrics.LittDBMetrics,
	) (litt.ManagedTable, error) {
		return memtable.NewMemTable(config, name), nil
	}

	return littbuilder.NewDBUnsafe(config, tb)
}

func buildMemKeyDiskDB(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)
	config.KeymapType = keymap.MemKeymapType
	config.WriteCacheSize = 1000
	config.TargetSegmentFileSize = 100
	config.ShardingFactor = 4
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	return littbuilder.NewDB(config)
}

func buildLevelDBDiskDB(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafeLevelDBKeymapType
	config.WriteCacheSize = 1000
	config.TargetSegmentFileSize = 100
	config.ShardingFactor = 4
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	return littbuilder.NewDB(config)
}

func buildLevelDBDiskDBWithFlushLimiter(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafeLevelDBKeymapType
	config.WriteCacheSize = 1000
	config.TargetSegmentFileSize = 100
	config.ShardingFactor = 4
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true
	config.MinimumFlushInterval = 50 * time.Millisecond

	db, err := littbuilder.NewDB(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build levelDB: %w", err)
	}
	return db, nil
}

func randomDBOperationsTest(t *testing.T, builder *dbBuilder) {
	rand := random.NewTestRandom()

	directory := t.TempDir()

	db, err := builder.builder(t, directory)
	require.NoError(t, err)

	tableCount := rand.Int32Range(8, 16)
	tableNames := make([]string, 0, tableCount)
	for i := int32(0); i < tableCount; i++ {
		tableNames = append(tableNames, fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8)))
	}

	// first key is table name, second key is the key in the kv-pair
	expectedValues := make(map[string]map[string][]byte)
	for _, tableName := range tableNames {
		expectedValues[tableName] = make(map[string][]byte)
	}

	iterations := 1000
	for i := 0; i < iterations; i++ {

		// Write some data.
		tableName := tableNames[rand.Intn(len(tableNames))]
		table, err := db.GetTable(tableName)
		require.NoError(t, err)

		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[tableName][string(key)] = value
		} else {
			batch := make([]*types.KVPair, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.KVPair{Key: key, Value: value})
				expectedValues[tableName][string(key)] = value
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Once in a while, flush tables.
		if rand.BoolWithProbability(0.1) {
			for _, tableName := range tableNames {
				table, err = db.GetTable(tableName)
				require.NoError(t, err)
				err = table.Flush()
				require.NoError(t, err)
			}
		}

		// Once in a while, sleep for a short time. For tables that do garbage collection, the garbage
		// collection interval has been configured to be 1ms. Sleeping 5ms should be enough to give
		// the garbage collector a chance to run.
		if rand.BoolWithProbability(0.01) {
			time.Sleep(5 * time.Millisecond)
		}

		// Once in a while, scan the tables and verify that all expected values are present.
		// Don't do this every time for the sake of test runtime.
		if rand.BoolWithProbability(0.01) || i == iterations-1 /* always check on the last iteration */ {
			for tableName, tableValues := range expectedValues {
				table, err := db.GetTable(tableName)
				require.NoError(t, err)

				for expectedKey, expectedValue := range tableValues {
					value, ok, err := table.Get([]byte(expectedKey))
					require.NoError(t, err)
					require.True(t, ok)
					require.Equal(t, expectedValue, value)
				}
			}
		}
	}

	err = db.Destroy()
	require.NoError(t, err)

	// ensure that the test directory is empty
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRandomDBOperations(t *testing.T) {
	t.Parallel()
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			randomDBOperationsTest(t, builder)
		})
	}
}

// Test with flush limiting enabled. This will be slower for the unit test data access pattern, but we need to
// exercise the code pathways.
func TestRandomDBOperationsWithFlushLimiter(t *testing.T) {
	t.Parallel()
	randomDBOperationsTest(t, flushLimitedBuilder)
}

func dbRestartTest(t *testing.T, builder *dbBuilder) {
	rand := random.NewTestRandom()

	directory := t.TempDir()

	db, err := builder.builder(t, directory)
	require.NoError(t, err)

	tableCount := rand.Int32Range(8, 16)
	tableNames := make([]string, 0, tableCount)
	for i := int32(0); i < tableCount; i++ {
		tableNames = append(tableNames, fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8)))
	}

	// first key is table name, second key is the key in the kv-pair
	expectedValues := make(map[string]map[string][]byte)
	for _, tableName := range tableNames {
		expectedValues[tableName] = make(map[string][]byte)
	}

	iterations := 1000
	restartIteration := iterations/2 + int(rand.Int64Range(-10, 10))

	for i := 0; i < iterations; i++ {
		// Somewhere in the middle of the test, restart the db.
		if i == restartIteration {
			err = db.Close()
			require.NoError(t, err)

			db, err = builder.builder(t, directory)
			require.NoError(t, err)

			// Do a full scan of the table to verify that all expected values are still present.
			for tableName, tableValues := range expectedValues {
				table, err := db.GetTable(tableName)
				require.NoError(t, err)

				for expectedKey, expectedValue := range tableValues {
					value, ok, err := table.Get([]byte(expectedKey))
					require.NoError(t, err)
					require.True(t, ok)
					require.Equal(t, expectedValue, value)
				}
			}
		}

		// Write some data.
		tableName := tableNames[rand.Intn(len(tableNames))]
		table, err := db.GetTable(tableName)
		require.NoError(t, err)

		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[tableName][string(key)] = value
		} else {
			batch := make([]*types.KVPair, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.KVPair{Key: key, Value: value})
				expectedValues[tableName][string(key)] = value
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Once in a while, flush tables.
		if rand.BoolWithProbability(0.1) {
			for _, tableName := range tableNames {
				table, err = db.GetTable(tableName)
				require.NoError(t, err)
				err = table.Flush()
				require.NoError(t, err)
			}
		}

		// Once in a while, sleep for a short time. For tables that do garbage collection, the garbage
		// collection interval has been configured to be 1ms. Sleeping 5ms should be enough to give
		// the garbage collector a chance to run.
		if rand.BoolWithProbability(0.01) {
			time.Sleep(5 * time.Millisecond)
		}

		// Once in a while, scan the tables and verify that all expected values are present.
		// Don't do this every time for the sake of test runtime.
		if rand.BoolWithProbability(0.01) || i == iterations-1 /* always check on the last iteration */ {
			for tableName, tableValues := range expectedValues {
				table, err := db.GetTable(tableName)
				require.NoError(t, err)

				for expectedKey, expectedValue := range tableValues {
					value, ok, err := table.Get([]byte(expectedKey))
					require.NoError(t, err)
					require.True(t, ok)
					require.Equal(t, expectedValue, value)
				}
			}
		}
	}

	err = db.Destroy()
	require.NoError(t, err)

	// ensure that the test directory is empty
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestDBRestart(t *testing.T) {
	t.Parallel()
	for _, builder := range restartableBuilders {
		t.Run(builder.name, func(t *testing.T) {
			dbRestartTest(t, builder)
		})
	}
}
