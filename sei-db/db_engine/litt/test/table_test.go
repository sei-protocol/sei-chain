package test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/common/cache"
	"github.com/Layr-Labs/eigenda/litt"
	tablecache "github.com/Layr-Labs/eigenda/litt/cache"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/memtable"
	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

type tableBuilder struct {
	name    string
	builder func(clock func() time.Time, name string, path string) (litt.ManagedTable, error)
}

// This test executes against different table implementations.
var tableBuilders = []*tableBuilder{
	{
		"memtable",
		buildMemTable,
	},
	{
		"cached memtable",
		buildCachedMemTable,
	},
	{
		"mem keymap disk table",
		buildMemKeyDiskTable,
	},
	{
		"cached mem keymap disk table",
		buildCachedMemKeyDiskTable,
	},
	{
		"leveldb keymap disk table",
		buildLevelDBKeyDiskTable,
	},
	{
		"cached leveldb keymap disk table",
		buildCachedLevelDBKeyDiskTable,
	},
}

var noCacheTableBuilders = []*tableBuilder{
	{
		"memtable",
		buildMemTable,
	},
	{
		"mem keymap disk table",
		buildMemKeyDiskTable,
	},
	{
		"leveldb keymap disk table",
		buildLevelDBKeyDiskTable,
	},
}

func buildMemTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	config, err := litt.DefaultConfig(path)
	config.Clock = clock
	config.GCPeriod = time.Millisecond

	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}

	return memtable.NewMemTable(config, name), nil
}

func setupKeymapTypeFile(keymapPath string, keymapType keymap.KeymapType) (*keymap.KeymapTypeFile, error) {
	exists, err := keymap.KeymapFileExists(keymapPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check if keymap file exists: %w", err)
	}
	var keymapTypeFile *keymap.KeymapTypeFile
	if exists {
		keymapTypeFile, err = keymap.LoadKeymapTypeFile(keymapPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load keymap type file: %w", err)
		}
	} else {
		err = os.MkdirAll(keymapPath, 0755)
		if err != nil {
			return nil, fmt.Errorf("failed to create keymap directory: %w", err)
		}
		keymapTypeFile = keymap.NewKeymapTypeFile(keymapPath, keymapType)
		err = keymapTypeFile.Write()
		if err != nil {
			return nil, fmt.Errorf("failed to create keymap type file: %w", err)
		}
	}

	return keymapTypeFile, nil
}

func buildMemKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	logger := test.GetLogger()

	keymapPath := filepath.Join(path, name, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	if err != nil {
		return nil, fmt.Errorf("failed to load keymap type file: %w", err)
	}

	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed to create keymap: %w", err)
	}

	config, err := litt.DefaultConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	config.GCPeriod = time.Millisecond
	config.Clock = clock
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.SaltShaker = random.NewTestRandom().Rand
	config.TargetSegmentFileSize = 100 // intentionally use a very small segment size
	config.Logger = logger

	table, err := disktable.NewDiskTable(
		config,
		name,
		keys,
		keymapPath,
		keymapTypeFile,
		[]string{path},
		true,
		nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create disk table: %w", err)
	}

	return table, nil
}

func buildLevelDBKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	logger := test.GetLogger()

	keymapPath := filepath.Join(path, name, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	if err != nil {
		return nil, fmt.Errorf("failed to load keymap type file: %w", err)
	}

	keys, _, err := keymap.NewUnsafeLevelDBKeymap(logger, keymapPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create keymap: %w", err)
	}

	config, err := litt.DefaultConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	config.GCPeriod = time.Millisecond
	config.Clock = clock
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.SaltShaker = random.NewTestRandom().Rand
	config.TargetSegmentFileSize = 100 // intentionally use a very small segment size
	config.Logger = logger

	table, err := disktable.NewDiskTable(
		config,
		name,
		keys,
		keymapPath,
		keymapTypeFile,
		[]string{path},
		true,
		nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create disk table: %w", err)
	}

	return table, nil
}

func buildCachedMemTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	baseTable, err := buildMemTable(clock, name, path)
	if err != nil {
		return nil, err
	}

	writeCache := cache.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)
	readCache := cache.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)

	return tablecache.NewCachedTable(baseTable, writeCache, readCache, nil), nil
}

func buildCachedMemKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	baseTable, err := buildMemKeyDiskTable(clock, name, path)
	if err != nil {
		return nil, err
	}

	writeCache := cache.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)
	readCache := cache.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)

	return tablecache.NewCachedTable(baseTable, writeCache, readCache, nil), nil
}

func buildCachedLevelDBKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	baseTable, err := buildLevelDBKeyDiskTable(clock, name, path)
	if err != nil {
		return nil, err
	}

	writeCache := cache.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)
	readCache := cache.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)

	return tablecache.NewCachedTable(baseTable, writeCache, readCache, nil), nil
}

func randomTableOperationsTest(t *testing.T, tableBuilder *tableBuilder) {
	rand := random.NewTestRandom()

	directory := t.TempDir()

	tableName := rand.String(8)
	table, err := tableBuilder.builder(time.Now, tableName, directory)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	require.Equal(t, tableName, table.Name())

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
			batch := make([]*types.KVPair, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.KVPair{Key: key, Value: value})
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
				ok, err := table.Exists([]byte(expectedKey))
				require.NoError(t, err)
				require.True(t, ok)
				value, ok, err := table.Get([]byte(expectedKey))
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, expectedValue, value)
			}

			// Try fetching a value that isn't in the table.
			nonExistentKey := rand.PrintableVariableBytes(32, 64)
			ok, err := table.Exists(nonExistentKey)
			require.NoError(t, err)
			require.False(t, ok)
			_, ok, err = table.Get(nonExistentKey)
			require.NoError(t, err)
			require.False(t, ok)
		}
	}

	err = table.Destroy()
	require.NoError(t, err)

	// ensure that the test directory is empty
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRandomTableOperations(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			randomTableOperationsTest(t, tb)
		})
	}
}

func garbageCollectionTest(t *testing.T, tableBuilder *tableBuilder) {
	rand := random.NewTestRandom()

	directory := t.TempDir()

	startTime := rand.Time()

	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&startTime)

	clock := func() time.Time {
		return *fakeTime.Load()
	}

	tableName := rand.String(8)
	table, err := tableBuilder.builder(clock, tableName, directory)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	ttlSeconds := rand.Int32Range(20, 30)
	ttl := time.Duration(ttlSeconds) * time.Second
	err = table.SetTTL(ttl)
	require.NoError(t, err)

	require.Equal(t, tableName, table.Name())

	expectedValues := make(map[string][]byte)
	creationTimes := make(map[string]time.Time)
	expiredValues := make(map[string][]byte)

	iterations := 1000
	for i := 0; i < iterations; i++ {

		// Advance the clock.
		now := *fakeTime.Load()
		secondsToAdvance := rand.Float64Range(0.0, 1.0)
		newTime := now.Add(time.Duration(secondsToAdvance * float64(time.Second)))
		fakeTime.Store(&newTime)

		// Write some data.
		batchSize := rand.Int32Range(1, 10)

		if batchSize == 1 {
			key := rand.PrintableVariableBytes(32, 64)
			value := rand.PrintableVariableBytes(1, 128)
			err = table.Put(key, value)
			require.NoError(t, err)
			expectedValues[string(key)] = value
			creationTimes[string(key)] = newTime
		} else {
			batch := make([]*types.KVPair, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.KVPair{Key: key, Value: value})
				expectedValues[string(key)] = value
				creationTimes[string(key)] = newTime
			}
			err = table.PutBatch(batch)
			require.NoError(t, err)
		}

		// Flush the table.
		err = table.Flush()
		require.NoError(t, err)

		// Once in a while, change the TTL. To avoid introducing test flakiness, only decrease the TTL
		// (increasing the TTL risks causing the expected deletions as tracked by this test to get out
		// of sync with what the table is doing)
		if rand.BoolWithProbability(0.01) {
			ttlSeconds -= 1
			ttl = time.Duration(ttlSeconds) * time.Second
			err = table.SetTTL(ttl)
			require.NoError(t, err)
		}

		// Once in a while, pause for a brief moment to give the garbage collector a chance to do work in the
		// background. This is not required for the test to pass.
		if rand.BoolWithProbability(0.01) {
			time.Sleep(5 * time.Millisecond)
		}

		// Once in a while, scan the table and verify that all expected values are present.
		// Don't do this every time for the sake of test runtime.
		if rand.BoolWithProbability(0.01) || i == iterations-1 /* always check on the last iteration */ {
			// Remove expired values from the expected values.
			newlyExpiredKeys := make([]string, 0)
			for key, creationTime := range creationTimes {
				if newTime.Sub(creationTime) > ttl {
					newlyExpiredKeys = append(newlyExpiredKeys, key)
				}
			}
			for _, key := range newlyExpiredKeys {
				expiredValues[key] = expectedValues[key]
				delete(expectedValues, key)
				delete(creationTimes, key)
			}

			// Check the keys that are expected to still be in the table
			for expectedKey, expectedValue := range expectedValues {
				value, ok, err := table.Get([]byte(expectedKey))
				require.NoError(t, err)
				require.True(t, ok, "key %s not found in table", expectedKey)
				require.Equal(t, expectedValue, value)
			}

			// Try fetching a value that isn't in the table.
			_, ok, err := table.Get(rand.PrintableVariableBytes(32, 64))
			require.NoError(t, err)
			require.False(t, ok)

			// Check the values that are expected to have been removed from the table
			// Garbage collection happens asynchronously, so we may need to wait for it to complete.
			test.AssertEventuallyTrue(t, func() bool {
				// keep a running sum of the unexpired data size. Some data may be unable to expire
				// due to sharing a file with data that is not yet ready to expire, so it's hard
				// to predict the exact quantity of unexpired data.
				//
				// Math:
				// - 100 bytes in each segment                   (test configuration)
				// - max value size of 128 bytes                 (test configuration)
				// - 4 bytes to store the length of the value    (default property)
				// - max bytes per segment: 100+128+4 = 232
				// - max number of segments per write is equal to max batch size, or 9
				// - max unexpired data size = 9 * 232 = 2088
				unexpiredDataSize := 0

				for key, expectedValue := range expiredValues {
					value, ok, err := table.Get([]byte(key))
					require.NoError(t, err)
					if !ok {
						// value is not present in the table
						continue
					}

					// If the value has not yet been deleted, it should at least return the expected value.
					require.Equal(t, expectedValue, value, "unexpected value for key %s", key)

					unexpiredDataSize += len(value) + 4 // 4 bytes stores the length of the value
				}

				// This check passes if the unexpired data size is less than or equal to the maximum plausible
				// size of unexpired data. If working as expected, this should always happen within a reasonable
				// amount of time.
				return unexpiredDataSize <= 2088
			}, time.Second)
		}
	}

	err = table.Destroy()
	require.NoError(t, err)

	// ensure that the test directory is empty
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestGarbageCollection(t *testing.T) {
	t.Parallel()
	for _, tb := range noCacheTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			garbageCollectionTest(t, tb)
		})
	}
}

func TestInvalidTableName(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()

	config, err := litt.DefaultConfig(directory)
	require.NoError(t, err)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableName := "invalid name"
	table, err := db.GetTable(tableName)
	require.Error(t, err)
	require.Nil(t, table)

	tableName = "invalid/name"
	table, err = db.GetTable(tableName)
	require.Error(t, err)
	require.Nil(t, table)

	tableName = ""
	table, err = db.GetTable(tableName)
	require.Error(t, err)
	require.Nil(t, table)
}
