package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestPrune(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	rootPathCount := rand.Uint64Range(2, 5)
	rootPaths := make([]string, rootPathCount)
	for i := uint64(0); i < rootPathCount; i++ {
		rootPaths[i] = path.Join(testDirectory, fmt.Sprintf("root-%d", i))
	}

	// Use a standard test configuration for LittDB.
	config, err := litt.DefaultConfig(rootPaths...)
	require.NoError(t, err)
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.ShardingFactor = uint32(rand.Uint64Range(rootPathCount, 2*rootPathCount))
	config.TargetSegmentFileSize = 100

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint64Range(2, 5)
	tables := make(map[string]litt.Table, tableCount)
	for i := uint64(0); i < tableCount; i++ {
		tableName := fmt.Sprintf("table-%d", i)
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
	}

	// map from table name to keys to values
	expectedData := make(map[string]map[string][]byte)
	for _, table := range tables {
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Write some data into the DB.
	for i := 0; i < 1000; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		tableName := fmt.Sprintf("table-%d", tableIndex)
		table := tables[tableName]

		key := rand.String(32)
		value := rand.PrintableVariableBytes(1, 100)

		err = table.Put([]byte(key), value)
		require.NoError(t, err)

		expectedData[tableName][key] = value
	}

	// Flush all tables to ensure data is written to disk.
	for _, table := range tables {
		err = table.Flush()
		require.NoError(t, err)
	}

	// Close the DB. Once this is done, override the timestamps on some of the segment files.
	// We can then ask prune() to get rid of these segments without fear of race conditions.
	err = db.Close()
	require.NoError(t, err)

	// After pruning, the segment indexes in this map should be the lowest segment index that we keep for each table.
	firstSegmentIndexToKeepByTable := make(map[string]uint32)
	// A map from table name a set of keys that are expected to be pruned.
	expectedPrunedKeys := make(map[string]map[string]struct{})

	// This is the time we will assign to the "old" segments that we want to prune.
	sixHoursAgo := uint64(time.Now().Add(-6 * time.Hour).Nanosecond())

	for tableName := range tables {
		segmentPaths, err := segment.BuildSegmentPaths(rootPaths, "", tableName)
		require.NoError(t, err)

		lowSegmentIndex, highSegmentIndex, segments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			segmentPaths,
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		firstSegmentIndexToKeep := lowSegmentIndex + (highSegmentIndex-lowSegmentIndex)/2
		firstSegmentIndexToKeepByTable[tableName] = firstSegmentIndexToKeep

		for i := lowSegmentIndex; i < firstSegmentIndexToKeep; i++ {
			seg := segments[i]
			metadataPath := seg.GetMetadataFilePath()

			// Overwrite the old metadata file. The timestamp is encoded at [24:32] in nanoseconds since the epoch.
			data, err := os.ReadFile(metadataPath)
			require.NoError(t, err)
			binary.BigEndian.PutUint64(data[24:32], sixHoursAgo)

			// write the modified metadata file back to disk.
			err = os.WriteFile(metadataPath, data, 0644)
			require.NoError(t, err)

			// Record the keys in this segment. We shouldn't see them after pruning.
			segmentKeys, err := seg.GetKeys()
			require.NoError(t, err)
			for _, key := range segmentKeys {
				if _, exists := expectedPrunedKeys[tableName]; !exists {
					expectedPrunedKeys[tableName] = make(map[string]struct{})
				}
				expectedPrunedKeys[tableName][string(key.Key)] = struct{}{}
			}
		}
	}

	// Now that we've doctored the segment files, tell prune to delete segments older than 1 hour.
	// In a technical sense there is a race condition in this test, but since the unit test panel
	// will time out long before 1 hour elapses, in practicality it can never be observed.
	err = prune(logger, rootPaths, []string{}, 60*60 /* seconds */, false)
	require.NoError(t, err)

	// Reopen the DB and verify its contents.
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)

	for tableName := range tables {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
	}

	for tableName, expected := range expectedData {
		for key, value := range expected {
			actual, ok, err := tables[tableName].Get([]byte(key))
			require.NoError(t, err)

			if _, pruned := expectedPrunedKeys[tableName][key]; pruned {
				// The key should have been pruned.
				require.False(t, ok)
				require.Nil(t, actual)
			} else {
				// The key should still exist.
				require.True(t, ok)
				require.Equal(t, value, actual)
			}
		}
	}

	// tear down
	err = db.Close()
	require.NoError(t, err)
}

func TestPruneSubset(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := test.GetLogger()
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	rootPathCount := rand.Uint64Range(2, 5)
	rootPaths := make([]string, rootPathCount)
	for i := uint64(0); i < rootPathCount; i++ {
		rootPaths[i] = path.Join(testDirectory, fmt.Sprintf("root-%d", i))
	}

	// Use a standard test configuration for LittDB.
	config, err := litt.DefaultConfig(rootPaths...)
	require.NoError(t, err)
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.ShardingFactor = uint32(rand.Uint64Range(rootPathCount, 2*rootPathCount))
	config.TargetSegmentFileSize = 100

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableCount := rand.Uint64Range(2, 5)
	tables := make(map[string]litt.Table, tableCount)
	// we will only prune data from these tables.
	tablesToPrune := make([]string, 0, tableCount/2)
	tablesToPruneSet := make(map[string]struct{}, tableCount/2)
	for i := uint64(0); i < tableCount; i++ {
		tableName := fmt.Sprintf("table-%d", i)
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
		if i%2 == 0 {
			// Only prune even-numbered tables.
			tablesToPrune = append(tablesToPrune, tableName)
			tablesToPruneSet[tableName] = struct{}{}
		}
	}

	// map from table name to keys to values
	expectedData := make(map[string]map[string][]byte)
	for _, table := range tables {
		expectedData[table.Name()] = make(map[string][]byte)
	}

	// Write some data into the DB.
	for i := 0; i < 1000; i++ {
		tableIndex := rand.Uint64Range(0, tableCount)
		tableName := fmt.Sprintf("table-%d", tableIndex)
		table := tables[tableName]

		key := rand.String(32)
		value := rand.PrintableVariableBytes(1, 100)

		err = table.Put([]byte(key), value)
		require.NoError(t, err)

		expectedData[tableName][key] = value
	}

	// Flush all tables to ensure data is written to disk.
	for _, table := range tables {
		err = table.Flush()
		require.NoError(t, err)
	}

	// Close the DB. Once this is done, override the timestamps on some of the segment files.
	// We can then ask prune() to get rid of these segments without fear of race conditions.
	err = db.Close()
	require.NoError(t, err)

	// After pruning, the segment indexes in this map should be the lowest segment index that we keep for each table.
	firstSegmentIndexToKeepByTable := make(map[string]uint32)
	// A map from table name a set of keys that are expected to be pruned.
	expectedPrunedKeys := make(map[string]map[string]struct{})

	// This is the time we will assign to the "old" segments that we want to prune.
	sixHoursAgo := uint64(time.Now().Add(-6 * time.Hour).Nanosecond())

	for tableName := range tables {
		segmentPaths, err := segment.BuildSegmentPaths(rootPaths, "", tableName)
		require.NoError(t, err)

		lowSegmentIndex, highSegmentIndex, segments, err := segment.GatherSegmentFiles(
			logger,
			errorMonitor,
			segmentPaths,
			false,
			time.Now(),
			false,
			false)
		require.NoError(t, err)

		firstSegmentIndexToKeep := lowSegmentIndex + (highSegmentIndex-lowSegmentIndex)/2
		firstSegmentIndexToKeepByTable[tableName] = firstSegmentIndexToKeep

		for i := lowSegmentIndex; i < firstSegmentIndexToKeep; i++ {
			seg := segments[i]
			metadataPath := seg.GetMetadataFilePath()

			// Overwrite the old metadata file. The timestamp is encoded at [24:32] in nanoseconds since the epoch.
			data, err := os.ReadFile(metadataPath)
			require.NoError(t, err)
			binary.BigEndian.PutUint64(data[24:32], sixHoursAgo)

			// write the modified metadata file back to disk.
			err = os.WriteFile(metadataPath, data, 0644)
			require.NoError(t, err)

			// Record the keys in this segment. We shouldn't see them after pruning.
			if _, pruneTable := tablesToPruneSet[tableName]; pruneTable {
				segmentKeys, err := seg.GetKeys()
				require.NoError(t, err)
				for _, key := range segmentKeys {
					if _, exists := expectedPrunedKeys[tableName]; !exists {
						expectedPrunedKeys[tableName] = make(map[string]struct{})
					}
					expectedPrunedKeys[tableName][string(key.Key)] = struct{}{}
				}
			}

		}
	}

	// Now that we've doctored the segment files, tell prune to delete segments older than 1 hour.
	// In a technical sense there is a race condition in this test, but since the unit test panel
	// will time out long before 1 hour elapses, in practicality it can never be observed.
	err = prune(logger, rootPaths, tablesToPrune, 60*60 /* seconds */, false)
	require.NoError(t, err)

	// Reopen the DB and verify its contents.
	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)

	for tableName := range tables {
		table, err := db.GetTable(tableName)
		require.NoError(t, err)
		tables[tableName] = table
	}

	for tableName, expected := range expectedData {
		for key, value := range expected {
			actual, ok, err := tables[tableName].Get([]byte(key))
			require.NoError(t, err)

			if _, pruned := expectedPrunedKeys[tableName][key]; pruned {
				// The key should have been pruned.
				require.False(t, ok)
				require.Nil(t, actual)
			} else {
				// The key should still exist.
				require.True(t, ok)
				require.Equal(t, value, actual)
			}
		}
	}

	// tear down
	err = db.Close()
	require.NoError(t, err)
}
