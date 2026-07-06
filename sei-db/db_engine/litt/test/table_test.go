package test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

type tableBuilder struct {
	name    string
	builder func(clock func() time.Time, name string, path string) (litt.ManagedTable, error)
}

// This test executes against different table implementations.
var tableBuilders = []*tableBuilder{
	{
		"mem keymap disk table",
		buildMemKeyDiskTable,
	},
	{
		"cached mem keymap disk table",
		buildCachedMemKeyDiskTable,
	},
	{
		"pebbledb keymap disk table",
		buildPebbleDBKeyDiskTable,
	},
	{
		"cached pebbledb keymap disk table",
		buildCachedPebbleDBKeyDiskTable,
	},
}

var noCacheTableBuilders = []*tableBuilder{
	{
		"mem keymap disk table",
		buildMemKeyDiskTable,
	},
	{
		"pebbledb keymap disk table",
		buildPebbleDBKeyDiskTable,
	},
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

	logger := slog.Default()

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
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.TargetSegmentFileSize = 100 // intentionally use a very small segment size

	runtimeConfig := litt.DefaultRuntimeConfig()
	runtimeConfig.Clock = clock
	runtimeConfig.Logger = logger

	table, err := disktable.NewDiskTable(
		config,
		runtimeConfig,
		name,
		litt.DefaultTableConfig(name),
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

func buildPebbleDBKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	logger := slog.Default()

	keymapPath := filepath.Join(path, name, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	if err != nil {
		return nil, fmt.Errorf("failed to load keymap type file: %w", err)
	}

	keys, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create keymap: %w", err)
	}

	config, err := litt.DefaultConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	config.GCPeriod = time.Millisecond
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.TargetSegmentFileSize = 100 // intentionally use a very small segment size

	runtimeConfig := litt.DefaultRuntimeConfig()
	runtimeConfig.Clock = clock
	runtimeConfig.Logger = logger

	table, err := disktable.NewDiskTable(
		config,
		runtimeConfig,
		name,
		litt.DefaultTableConfig(name),
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

func buildCachedMemKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	baseTable, err := buildMemKeyDiskTable(clock, name, path)
	if err != nil {
		return nil, err
	}

	writeCache := util.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)
	readCache := util.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)

	return dbcache.NewCachedTable(baseTable, writeCache, readCache, nil), nil
}

func buildCachedPebbleDBKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (litt.ManagedTable, error) {

	baseTable, err := buildPebbleDBKeyDiskTable(clock, name, path)
	if err != nil {
		return nil, err
	}

	writeCache := util.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)
	readCache := util.NewFIFOCache[string, []byte](500, func(k string, v []byte) uint64 {
		return uint64(len(k) + len(v))
	}, nil)

	return dbcache.NewCachedTable(baseTable, writeCache, readCache, nil), nil
}

func randomTableOperationsTest(t *testing.T, tableBuilder *tableBuilder) {
	rand := util.NewTestRandom()

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

	err = table.Drop()
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
	rand := util.NewTestRandom()

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
	// Safety net: if an assertion fails mid-test, Drop stops the background GC goroutine before the
	// t.TempDir cleanup removes the directory out from under it. Drop is idempotent (guarded by a CAS),
	// so the explicit Drop at the end of the happy path makes this a no-op.
	defer func() { _ = table.Drop() }()

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
			batch := make([]*types.PutRequest, 0, batchSize)
			for j := int32(0); j < batchSize; j++ {
				key := rand.PrintableVariableBytes(32, 64)
				value := rand.PrintableVariableBytes(1, 128)
				batch = append(batch, &types.PutRequest{Key: key, Value: value})
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

			// Check the values that are expected to have been removed from the table. Drive a synchronous GC
			// pass rather than waiting on the background collector: RunGC runs collection, the keymap-delete
			// sync, and file reclamation to completion, so once it returns every currently-expired sealed
			// segment has been collected and its keymap entries deleted. This makes the check deterministic
			// (no reliance on the 1ms ticker catching up within a real-time window), so it can only fail if
			// collection is actually broken.
			require.NoError(t, table.RunGC())

			// Keep a running sum of the unexpired data size. Some expired data may still be present because it
			// shares a segment with data that is not yet ready to expire (segments expire as a unit), so the
			// exact quantity of lingering data is not predictable; we assert it stays within the plausible bound.
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

			require.LessOrEqual(t, unexpiredDataSize, 2088)
		}
	}

	err = table.Drop()
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
	table, err := db.BuildTable(litt.DefaultTableConfig(tableName))
	require.Error(t, err)
	require.Nil(t, table)

	tableName = "invalid/name"
	table, err = db.BuildTable(litt.DefaultTableConfig(tableName))
	require.Error(t, err)
	require.Nil(t, table)

	tableName = ""
	table, err = db.BuildTable(litt.DefaultTableConfig(tableName))
	require.Error(t, err)
	require.Nil(t, table)
}

// secondaryKeyBasicsTest runs against every table implementation registered in tableBuilders. It
// verifies that secondary keys behave like first-class keys at the Table interface: Put accepts
// them, Get returns the correct sub-range bytes both before and after Flush, Exists reports them
// as present, and KeyCount counts them.
func secondaryKeyBasicsTest(t *testing.T, tb *tableBuilder) {
	rand := util.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)
	table, err := tb.builder(time.Now, tableName, directory)
	require.NoError(t, err)

	value := []byte("the quick brown fox")
	primary := []byte("primary")
	sk1 := &types.SecondaryKey{Key: []byte("quick"), Offset: 4, Length: 5}
	sk2 := &types.SecondaryKey{Key: []byte("alias"), Offset: 0, Length: uint32(len(value))}

	require.NoError(t, table.Put(primary, value, sk1, sk2))

	verify := func(stage string) {
		t.Helper()
		got, ok, err := table.Get(primary)
		require.NoError(t, err, stage)
		require.True(t, ok, stage)
		require.Equal(t, value, got, stage)

		ok, err = table.Exists(sk1.Key)
		require.NoError(t, err, stage)
		require.True(t, ok, stage)
		got, ok, err = table.Get(sk1.Key)
		require.NoError(t, err, stage)
		require.True(t, ok, stage)
		require.Equal(t, value[sk1.Offset:sk1.Offset+sk1.Length], got, stage)

		got, ok, err = table.Get(sk2.Key)
		require.NoError(t, err, stage)
		require.True(t, ok, stage)
		require.Equal(t, value, got, stage)

		require.EqualValues(t, 3, table.KeyCount(), stage)
	}

	verify("before flush")
	require.NoError(t, table.Flush())
	verify("after flush")

	require.NoError(t, table.Drop())
}

func TestSecondaryKeyBasics(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		tb := tb
		t.Run(tb.name, func(t *testing.T) {
			t.Parallel()
			secondaryKeyBasicsTest(t, tb)
		})
	}
}
