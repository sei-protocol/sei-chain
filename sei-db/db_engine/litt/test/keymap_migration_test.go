package test

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// Tests migration from one type of Keymap to another.
func TestKeymapMigration(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	directory := t.TempDir()

	directoryCount := 8
	shardDirectories := make([]string, 0, directoryCount)
	for i := 0; i < directoryCount; i++ {
		shardDirectories = append(shardDirectories, path.Join(directory, rand.String(32)))
	}

	// Build the table using PebbleDBKeymap.
	config, err := litt.DefaultConfig(shardDirectories...)
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafePebbleDBKeymapType
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	tableConfig := litt.DefaultTableConfig("test")
	tableConfig.ShardingFactor = uint8(directoryCount)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	// Fill the table with some data.
	expectedValues := make(map[string][]byte)

	iterations := 1000
	for i := 0; i < iterations; i++ {

		// Write some data.
		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[string(key)] = value
		} else {
			batch := make([]*types.PutRequest, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.PutRequest{Key: key, Value: value})
				expectedValues[string(key)] = value
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Once in a while, flush the table.
		if rand.BoolWithProbability(0.1) {
			err = table.Flush()
			require.NoError(t, err)
		}

		// Once in a while, sleep for a short time. For tables that do garbage collection, the garbage
		// collection interval has been configured to be 1ms. Sleeping 5ms should be enough to give
		// the garbage collector a chance to run.
		if rand.BoolWithProbability(0.01) {
			time.Sleep(5 * time.Millisecond)
		}

		// Once in a while, scan the table and verify that all expected values are present.
		// Don't do this every time for the sake of test runtime.
		if rand.BoolWithProbability(0.01) || i == iterations-1 /* always check on the last iteration */ {
			for expectedKey, expectedValue := range expectedValues {
				value, ok, err := table.Get([]byte(expectedKey))
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, expectedValue, value)
			}

			// Try fetching a value that isn't in the table.
			_, ok, err := table.Get(rand.PrintableVariableBytes(32, 64))
			require.NoError(t, err)
			require.False(t, ok)
		}
	}

	// Shut down the table and move the keymap directory. There shouldn't be any problems caused by this.
	err = db.Close()
	require.NoError(t, err)

	// By default, the keymap will store its data inside directory 0
	keymapPath := path.Join(shardDirectories[0], "test", "keymap")
	newKeymapPath := path.Join(shardDirectories[int(rand.Int64Range(1, int64(directoryCount)))],
		"test", "keymap")

	err = os.Rename(keymapPath, newKeymapPath)
	require.NoError(t, err)

	// Reload the table and check the data
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err = db.BuildTable(tableConfig)
	require.NoError(t, err)

	for expectedKey, expectedValue := range expectedValues {
		value, ok, err := table.Get([]byte(expectedKey))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedValue, value)
	}

	// Close the table and reopen it using a MemKeymap
	err = db.Close()
	require.NoError(t, err)
	config.KeymapType = keymap.MemKeymapType

	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err = db.BuildTable(tableConfig)
	require.NoError(t, err)

	for expectedKey, expectedValue := range expectedValues {
		value, ok, err := table.Get([]byte(expectedKey))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedValue, value)
	}

	// The keymap data path should be empty.
	keymapDataPath := path.Join(newKeymapPath, keymap.KeymapDataDirectoryName)
	_, err = os.Stat(keymapDataPath)
	require.True(t, os.IsNotExist(err))

	// Close the table and reopen it using a PebbleDBKeymap
	err = db.Close()
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafePebbleDBKeymapType

	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err = db.BuildTable(tableConfig)
	require.NoError(t, err)

	for expectedKey, expectedValue := range expectedValues {
		value, ok, err := table.Get([]byte(expectedKey))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedValue, value)
	}

	err = db.Destroy()
	require.NoError(t, err)
}

func TestFailedKeymapMigration(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	directory := t.TempDir()

	directoryCount := 8
	shardDirectories := make([]string, 0, directoryCount)
	for i := 0; i < directoryCount; i++ {
		shardDirectories = append(shardDirectories, path.Join(directory, rand.String(32)))
	}

	// Build the table using PebbleDBKeymap.
	config, err := litt.DefaultConfig(shardDirectories...)
	require.NoError(t, err)
	config.KeymapType = keymap.UnsafePebbleDBKeymapType
	config.Fsync = false // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	tableConfig := litt.DefaultTableConfig("test")
	tableConfig.ShardingFactor = uint8(directoryCount)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	// Fill the table with some data.
	expectedValues := make(map[string][]byte)

	iterations := 1000
	for i := 0; i < iterations; i++ {

		// Write some data.
		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[string(key)] = value
		} else {
			batch := make([]*types.PutRequest, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.PutRequest{Key: key, Value: value})
				expectedValues[string(key)] = value
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Once in a while, flush the table.
		if rand.BoolWithProbability(0.1) {
			err = table.Flush()
			require.NoError(t, err)
		}

		// Once in a while, sleep for a short time. For tables that do garbage collection, the garbage
		// collection interval has been configured to be 1ms. Sleeping 5ms should be enough to give
		// the garbage collector a chance to run.
		if rand.BoolWithProbability(0.01) {
			time.Sleep(5 * time.Millisecond)
		}

		// Once in a while, scan the table and verify that all expected values are present.
		// Don't do this every time for the sake of test runtime.
		if rand.BoolWithProbability(0.01) || i == iterations-1 /* always check on the last iteration */ {
			for expectedKey, expectedValue := range expectedValues {
				value, ok, err := table.Get([]byte(expectedKey))
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, expectedValue, value)
			}

			// Try fetching a value that isn't in the table.
			_, ok, err := table.Get(rand.PrintableVariableBytes(32, 64))
			require.NoError(t, err)
			require.False(t, ok)
		}
	}

	err = db.Close()
	require.NoError(t, err)

	// Simulate a failed reload. A failed reload be identified by the missing "initialized" flag file.
	// By deleting the file, the DB is tricked into reloading the keymap.
	flagFilePath := path.Join(shardDirectories[0], "test", keymap.KeymapDirectoryName, keymap.KeymapInitializedFileName)

	exists, err := util.Exists(flagFilePath)
	require.NoError(t, err)
	require.True(t, exists)

	err = os.Remove(flagFilePath)
	require.NoError(t, err)

	// To verify that the migration works, manually load the old keymap and corrupt it. If things work as they should,
	// the keymap should be reloaded from disk, and the corrupted keymap should be deleted.
	pebbleDBPath := path.Join(shardDirectories[0], "test", keymap.KeymapDirectoryName, keymap.KeymapDataDirectoryName)
	pdb, err := pebble.Open(pebbleDBPath, &pebble.Options{})
	require.NoError(t, err)

	for key := range expectedValues {
		err = pdb.Set([]byte(key), []byte(fmt.Sprintf("%d", rand.Uint64())), pebble.NoSync)
		require.NoError(t, err)
	}

	err = pdb.Close()
	require.NoError(t, err)

	// Reload the table and check the data
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err = db.BuildTable(tableConfig)
	require.NoError(t, err)

	for expectedKey, expectedValue := range expectedValues {
		value, ok, err := table.Get([]byte(expectedKey))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedValue, value)
	}
}
