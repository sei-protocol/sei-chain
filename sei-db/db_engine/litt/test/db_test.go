package test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

type dbBuilder struct {
	name    string
	builder func(t *testing.T, tableDirectory string) (litt.DB, error)
	// tableConfig is the per-table config to pass to DB.BuildTable for tables created by this builder.
	tableConfig litt.TableConfig
}

// diskTableConfig is the per-table config used by the disk-backed test builders. These settings previously
// lived on the top-level litt.Config.
var diskTableConfig = litt.TableConfig{
	TTL:            0,
	ShardingFactor: 4,
	WriteCacheSize: 1000,
}

var builders = []*dbBuilder{
	{
		name:        "mem keymap disk table",
		builder:     buildMemKeyDiskDB,
		tableConfig: diskTableConfig,
	},
	{
		name:        "pebbleDB keymap disk table",
		builder:     buildPebbleDBDiskDB,
		tableConfig: diskTableConfig,
	},
}

var restartableBuilders = []*dbBuilder{
	{
		name:        "mem keymap disk table",
		builder:     buildMemKeyDiskDB,
		tableConfig: diskTableConfig,
	},
	{
		name:        "pebbleDB keymap disk table",
		builder:     buildPebbleDBDiskDB,
		tableConfig: diskTableConfig,
	},
}

var flushLimitedBuilder = &dbBuilder{
	name:        "pebbleDB keymap disk table with flush limiter",
	builder:     buildPebbleDBDiskDBWithFlushLimiter,
	tableConfig: diskTableConfig,
}

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

func buildMemKeyDiskDB(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)
	config.KeymapType = keymap.MemKeymapType
	config.TargetSegmentFileSize = 100
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	return littbuilder.NewDB(config)
}

func buildPebbleDBDiskDB(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafePebbleDBKeymapType
	config.TargetSegmentFileSize = 100
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	return littbuilder.NewDB(config)
}

func buildPebbleDBDiskDBWithFlushLimiter(t *testing.T, path string) (litt.DB, error) {
	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafePebbleDBKeymapType
	config.TargetSegmentFileSize = 100
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true
	config.MinimumFlushInterval = 50 * time.Millisecond

	db, err := littbuilder.NewDB(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build pebbleDB: %w", err)
	}
	return db, nil
}

func randomDBOperationsTest(t *testing.T, builder *dbBuilder) {
	rand := util.NewTestRandom()

	directory := t.TempDir()

	db, err := builder.builder(t, directory)
	require.NoError(t, err)

	tableCount := rand.Int32Range(8, 16)
	tableNames := make([]string, 0, tableCount)
	for i := int32(0); i < tableCount; i++ {
		tableNames = append(tableNames, fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8)))
	}

	tables := buildTables(t, db, builder.tableConfig, tableNames)

	// first key is table name, second key is the key in the kv-pair
	expectedValues := make(map[string]map[string][]byte)
	for _, tableName := range tableNames {
		expectedValues[tableName] = make(map[string][]byte)
	}

	iterations := 1000
	for i := 0; i < iterations; i++ {

		// Write some data.
		tableName := tableNames[rand.Intn(len(tableNames))]
		table := tables[tableName]

		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[tableName][string(key)] = value
		} else {
			batch := make([]*types.PutRequest, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.PutRequest{Key: key, Value: value})
				expectedValues[tableName][string(key)] = value
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Once in a while, flush tables.
		if rand.BoolWithProbability(0.1) {
			for _, tableName := range tableNames {
				err = tables[tableName].Flush()
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
				table := tables[tableName]

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
	rand := util.NewTestRandom()

	directory := t.TempDir()

	db, err := builder.builder(t, directory)
	require.NoError(t, err)

	tableCount := rand.Int32Range(8, 16)
	tableNames := make([]string, 0, tableCount)
	for i := int32(0); i < tableCount; i++ {
		tableNames = append(tableNames, fmt.Sprintf("table-%d-%s", i, rand.PrintableBytes(8)))
	}

	tables := buildTables(t, db, builder.tableConfig, tableNames)

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

			// Rebuild the table handles after restart.
			tables = buildTables(t, db, builder.tableConfig, tableNames)

			// Do a full scan of the table to verify that all expected values are still present.
			for tableName, tableValues := range expectedValues {
				table := tables[tableName]

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
		table := tables[tableName]

		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[tableName][string(key)] = value
		} else {
			batch := make([]*types.PutRequest, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.PutRequest{Key: key, Value: value})
				expectedValues[tableName][string(key)] = value
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Once in a while, flush tables.
		if rand.BoolWithProbability(0.1) {
			for _, tableName := range tableNames {
				err = tables[tableName].Flush()
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
				table := tables[tableName]

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

func dropTableTest(t *testing.T, builder *dbBuilder) {
	rand := util.NewTestRandom()

	directory := t.TempDir()

	db, err := builder.builder(t, directory)
	require.NoError(t, err)

	tableName := "to-be-dropped"
	cfg := builder.tableConfig
	cfg.Name = tableName

	table, err := db.BuildTable(cfg)
	require.NoError(t, err)

	// Write some data and confirm it is readable.
	key := rand.PrintableVariableBytes(32, 64)
	value := rand.PrintableVariableBytes(1, 128)
	require.NoError(t, table.Put(key, value))
	require.NoError(t, table.Flush())

	readValue, ok, err := table.Get(key)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, readValue)

	// Drop the table. Its on-disk data should be deleted.
	require.NoError(t, table.Drop())

	// Rebuilding the same name must succeed (the DB has forgotten the dropped table) and yield a fresh,
	// empty table.
	table, err = db.BuildTable(cfg)
	require.NoError(t, err)

	_, ok, err = table.Get(key)
	require.NoError(t, err)
	require.False(t, ok)

	// Closing the DB after a drop must not error.
	require.NoError(t, db.Close())
}

func TestDropTable(t *testing.T) {
	t.Parallel()
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			dropTableTest(t, builder)
		})
	}
}
