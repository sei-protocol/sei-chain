package litt

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	cache "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/datacache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/test"
	"github.com/stretchr/testify/require"
)

type integrationTableBuilder struct {
	name    string
	builder func(clock func() time.Time, name string, path string) (ManagedTable, error)
}

// This test executes against different table implementations.
var integrationTableBuilders = []*integrationTableBuilder{
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

var integrationNoCacheTableBuilders = []*integrationTableBuilder{
	{
		"mem keymap disk table",
		buildMemKeyDiskTable,
	},
	{
		"leveldb keymap disk table",
		buildLevelDBKeyDiskTable,
	},
}

func buildMemKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (ManagedTable, error) {

	logger := test.GetLogger()

	keymapPath := filepath.Join(path, name, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	if err != nil {
		return nil, fmt.Errorf("failed to load keymap type file: %w", err)
	}

	keys, _, err := keymap.NewMemKeymap(logger, "", true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create keymap: %w", err)
	}

	config, err := DefaultConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	config.GCPeriod = time.Millisecond
	config.Clock = clock
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.SaltShaker = test.NewTestRandom().Rand
	config.TargetSegmentFileSize = 100 // intentionally use a very small segment size
	config.Logger = logger

	table, err := newDiskTable(
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
	path string) (ManagedTable, error) {

	logger := test.GetLogger()

	keymapPath := filepath.Join(path, name, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	if err != nil {
		return nil, fmt.Errorf("failed to load keymap type file: %w", err)
	}

	keys, _, err := keymap.NewUnsafeLevelDBKeymap(logger, keymapPath, true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create keymap: %w", err)
	}

	config, err := DefaultConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	config.GCPeriod = time.Millisecond
	config.Clock = clock
	config.Fsync = false
	config.DoubleWriteProtection = true
	config.SaltShaker = test.NewTestRandom().Rand
	config.TargetSegmentFileSize = 100 // intentionally use a very small segment size
	config.Logger = logger

	table, err := newDiskTable(
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

func buildCachedMemKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (ManagedTable, error) {

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

	return newCachedTable(baseTable, writeCache, readCache, nil), nil
}

func buildCachedLevelDBKeyDiskTable(
	clock func() time.Time,
	name string,
	path string) (ManagedTable, error) {

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

	return newCachedTable(baseTable, writeCache, readCache, nil), nil
}

func randomTableOperationsTest(t *testing.T, integrationTableBuilder *integrationTableBuilder) {
	rand := test.NewTestRandom()

	directory := t.TempDir()

	tableName := rand.String(8)
	table, err := integrationTableBuilder.builder(time.Now, tableName, directory)
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

	err = table.Destroy()
	require.NoError(t, err)

	// ensure that the test directory is empty
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRandomTableOperations(t *testing.T) {
	t.Parallel()
	for _, tb := range integrationTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			randomTableOperationsTest(t, tb)
		})
	}
}

func garbageCollectionTest(t *testing.T, integrationTableBuilder *integrationTableBuilder) {
	rand := test.NewTestRandom()

	directory := t.TempDir()

	startTime := rand.Time()

	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&startTime)

	clock := func() time.Time {
		return *fakeTime.Load()
	}

	tableName := rand.String(8)
	table, err := integrationTableBuilder.builder(clock, tableName, directory)
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
	for _, tb := range integrationNoCacheTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			garbageCollectionTest(t, tb)
		})
	}
}

// secondaryKeyBasicTest verifies that Put() with secondary keys correctly stores subranges
// and that those subranges can be retrieved via Get() using the secondary key.
func secondaryKeyBasicTest(t *testing.T, builder *integrationTableBuilder) {
	rand := test.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)

	table, err := builder.builder(time.Now, tableName, directory)
	require.NoError(t, err)

	value := []byte("AAAAABBBBBCCCCCDDDDDEEEEE")
	primaryKey := rand.PrintableBytes(16)

	secondaryKeys := []*types.SecondaryKey{
		{Key: []byte("alias-B"), Offset: 5, Length: 5},
		{Key: []byte("alias-CD"), Offset: 10, Length: 10},
		{Key: []byte("alias-full"), Offset: 0, Length: uint32(len(value))},
		{Key: []byte("alias-single"), Offset: 24, Length: 1},
	}

	err = table.Put(primaryKey, value, secondaryKeys...)
	require.NoError(t, err)

	// Verify primary key returns full value.
	got, ok, err := table.Get(primaryKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, got)

	// Verify each secondary key returns the correct subrange.
	got, ok, err = table.Get([]byte("alias-B"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("BBBBB"), got)

	got, ok, err = table.Get([]byte("alias-CD"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("CCCCCDDDDD"), got)

	got, ok, err = table.Get([]byte("alias-full"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, got)

	got, ok, err = table.Get([]byte("alias-single"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("E"), got)

	// Exists should work for secondary keys.
	exists, err := table.Exists([]byte("alias-B"))
	require.NoError(t, err)
	require.True(t, exists)

	// Non-existent key should not be found.
	_, ok, err = table.Get([]byte("nonexistent"))
	require.NoError(t, err)
	require.False(t, ok)

	err = table.Destroy()
	require.NoError(t, err)
}

func TestSecondaryKeyBasic(t *testing.T) {
	t.Parallel()
	for _, tb := range integrationTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			secondaryKeyBasicTest(t, tb)
		})
	}
}

// secondaryKeyFlushAndReadTest verifies that secondary keys survive flushing to disk
// and can be read back from the on-disk segments.
func secondaryKeyFlushAndReadTest(t *testing.T, builder *integrationTableBuilder) {
	rand := test.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)

	table, err := builder.builder(time.Now, tableName, directory)
	require.NoError(t, err)

	value := []byte("0123456789abcdefghijklmnopqrstuvwxyz")

	err = table.Put(
		[]byte("primary"),
		value,
		&types.SecondaryKey{Key: []byte("digits"), Offset: 0, Length: 10},
		&types.SecondaryKey{Key: []byte("letters"), Offset: 10, Length: 26},
	)
	require.NoError(t, err)

	// Flush to disk to ensure data leaves the unflushed cache.
	err = table.Flush()
	require.NoError(t, err)

	// Read back after flush.
	got, ok, err := table.Get([]byte("primary"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, got)

	got, ok, err = table.Get([]byte("digits"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("0123456789"), got)

	got, ok, err = table.Get([]byte("letters"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("abcdefghijklmnopqrstuvwxyz"), got)

	err = table.Destroy()
	require.NoError(t, err)
}

func TestSecondaryKeyFlushAndRead(t *testing.T) {
	t.Parallel()
	for _, tb := range integrationTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			secondaryKeyFlushAndReadTest(t, tb)
		})
	}
}

// secondaryKeyPutBatchTest verifies that PutBatch correctly handles entries with secondary keys.
func secondaryKeyPutBatchTest(t *testing.T, builder *integrationTableBuilder) {
	rand := test.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)

	table, err := builder.builder(time.Now, tableName, directory)
	require.NoError(t, err)

	value1 := []byte("HEADBODY1TAIL")
	value2 := []byte("XXYYZZWW")

	batch := []*types.PutRequest{
		{
			Key:   []byte("pk1"),
			Value: value1,
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("pk1-head"), Offset: 0, Length: 4},
				{Key: []byte("pk1-body"), Offset: 4, Length: 5},
				{Key: []byte("pk1-tail"), Offset: 9, Length: 4},
			},
		},
		{
			Key:   []byte("pk2"),
			Value: value2,
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("pk2-first-half"), Offset: 0, Length: 4},
				{Key: []byte("pk2-second-half"), Offset: 4, Length: 4},
			},
		},
		{
			Key:   []byte("pk3-no-secondary"),
			Value: []byte("plain-value"),
		},
	}

	err = table.PutBatch(batch)
	require.NoError(t, err)

	err = table.Flush()
	require.NoError(t, err)

	// Verify primary keys.
	got, ok, err := table.Get([]byte("pk1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value1, got)

	got, ok, err = table.Get([]byte("pk2"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value2, got)

	got, ok, err = table.Get([]byte("pk3-no-secondary"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("plain-value"), got)

	// Verify secondary keys for pk1.
	got, ok, err = table.Get([]byte("pk1-head"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("HEAD"), got)

	got, ok, err = table.Get([]byte("pk1-body"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("BODY1"), got)

	got, ok, err = table.Get([]byte("pk1-tail"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("TAIL"), got)

	// Verify secondary keys for pk2.
	got, ok, err = table.Get([]byte("pk2-first-half"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("XXYY"), got)

	got, ok, err = table.Get([]byte("pk2-second-half"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("ZZWW"), got)

	err = table.Destroy()
	require.NoError(t, err)
}

func TestSecondaryKeyPutBatch(t *testing.T) {
	t.Parallel()
	for _, tb := range integrationTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			secondaryKeyPutBatchTest(t, tb)
		})
	}
}

// secondaryKeyRandomTest is a randomized integration test that mixes Put/PutBatch
// calls with and without secondary keys, periodically flushing and verifying all data.
func secondaryKeyRandomTest(t *testing.T, builder *integrationTableBuilder) {
	rand := test.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)

	table, err := builder.builder(time.Now, tableName, directory)
	require.NoError(t, err)

	type expectedEntry struct {
		value []byte
	}

	expected := make(map[string]*expectedEntry)

	iterations := 500
	for i := 0; i < iterations; i++ {
		value := rand.PrintableVariableBytes(20, 200)
		primaryKey := rand.PrintableVariableBytes(16, 32)

		secondaryKeyCount := rand.Int32Range(0, 5)
		var secondaryKeys []*types.SecondaryKey
		for j := int32(0); j < secondaryKeyCount; j++ {
			valueLen := uint32(len(value))
			offset := uint32(rand.Int32Range(0, int32(valueLen)))
			maxLength := valueLen - offset
			if maxLength == 0 {
				continue
			}
			length := uint32(rand.Int32Range(1, int32(maxLength)+1))

			sk := &types.SecondaryKey{
				Key:    rand.PrintableVariableBytes(16, 32),
				Offset: offset,
				Length: length,
			}
			secondaryKeys = append(secondaryKeys, sk)
			expected[string(sk.Key)] = &expectedEntry{value: value[offset : offset+length]}
		}

		useBatch := rand.BoolWithProbability(0.5)
		if useBatch {
			req := &types.PutRequest{
				Key:           primaryKey,
				Value:         value,
				SecondaryKeys: secondaryKeys,
			}
			err = table.PutBatch([]*types.PutRequest{req})
		} else {
			err = table.Put(primaryKey, value, secondaryKeys...)
		}
		require.NoError(t, err)
		expected[string(primaryKey)] = &expectedEntry{value: value}

		if rand.BoolWithProbability(0.1) {
			err = table.Flush()
			require.NoError(t, err)
		}

		if rand.BoolWithProbability(0.02) || i == iterations-1 {
			for key, entry := range expected {
				got, ok, err := table.Get([]byte(key))
				require.NoError(t, err)
				require.True(t, ok, "key %q not found", key)
				require.Equal(t, entry.value, got, "mismatch for key %q", key)
			}
		}
	}

	err = table.Destroy()
	require.NoError(t, err)
}

func TestSecondaryKeyRandom(t *testing.T) {
	t.Parallel()
	for _, tb := range integrationTableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			secondaryKeyRandomTest(t, tb)
		})
	}
}

func TestInvalidTableName(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()

	config, err := DefaultConfig(directory)
	require.NoError(t, err)

	db, err := NewDB(config)
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
