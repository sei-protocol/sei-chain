package test

import (
	"os"
	"testing"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestCache(t *testing.T) {
	rand := random.NewTestRandom()

	directory := t.TempDir()

	config, err := litt.DefaultConfig(directory)
	require.NoError(t, err)

	config.WriteCacheSize = rand.Uint64Range(1000, 2000)
	config.ReadCacheSize = rand.Uint64Range(1000, 2000)
	config.Fsync = false
	config.DoubleWriteProtection = true

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	table, err := db.GetTable("test_table")
	require.NoError(t, err)

	expectedValues := make(map[string][]byte)

	var firstKey []byte
	var firstValueSize uint64

	keySize := uint64(32)
	maxValueSize := uint64(50)

	// Write some values to the table. Stop before any values are evicted from the write cache.
	bytesWritten := uint64(0)
	for bytesWritten <= config.WriteCacheSize-keySize-maxValueSize {
		nextValueSize := rand.Uint64Range(1, maxValueSize)
		kvSize := keySize + nextValueSize

		bytesWritten += kvSize

		key := rand.PrintableBytes(int(keySize))
		value := rand.PrintableBytes(int(nextValueSize))

		if firstKey == nil {
			firstKey = key
			firstValueSize = nextValueSize
		}

		expectedValues[string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err)
	}
	err = table.Flush()
	require.NoError(t, err)

	// Read all values. All should be hot (i.e. in the read cache).
	for expectedKey, expectedValue := range expectedValues {
		// Only permit reading from the cache.
		value, ok, hot, err := table.CacheAwareGet([]byte(expectedKey), true)
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, hot)
		require.Equal(t, expectedValue, value)

		// Permit reading from disk. Since everything is in the cache, this should be functionally equivalent.
		value, ok, hot, err = table.CacheAwareGet([]byte(expectedKey), false)
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, hot)
		require.Equal(t, expectedValue, value)
	}

	// Write another value that is large enough to evict the first value. This should cause the first value to be
	// evicted from the write cache.
	key := rand.PrintableBytes(int(keySize))
	value := rand.PrintableBytes(int(maxValueSize))
	bytesWritten += keySize + maxValueSize
	expectedValues[string(key)] = value
	err = table.Put(key, value)
	require.NoError(t, err)

	// Read the first value. It should not be hot. For the first request, do not allow a trip to the cache.
	value, ok, hot, err := table.CacheAwareGet(firstKey, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.Nil(t, value)
	require.False(t, hot)

	// Try again, but allow a trip to the cache.
	value, ok, hot, err = table.CacheAwareGet(firstKey, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, hot)
	require.Equal(t, expectedValues[string(firstKey)], value)

	// Reading again should now result in a cache hit.
	value, ok, hot, err = table.CacheAwareGet(firstKey, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, hot)
	require.Equal(t, expectedValues[string(firstKey)], value)

	// Write enough values to push all previously written values out of the write cache.
	for bytesWritten < 5000 {
		nextValueSize := rand.Uint64Range(1, maxValueSize)
		kvSize := keySize + nextValueSize

		bytesWritten += kvSize

		key := rand.PrintableBytes(int(keySize))
		value := rand.PrintableBytes(int(nextValueSize))

		if firstKey == nil {
			firstKey = key
		}

		expectedValues[string(key)] = value
		err = table.Put(key, value)
		require.NoError(t, err)
	}
	err = table.Flush()
	require.NoError(t, err)

	// At this moment in time, the number of bytes in the cache should be less than the write cache size, plus that
	// of the first key which will be in the read cache. Verify that fact.
	maxCacheSize := config.WriteCacheSize + keySize + firstValueSize
	hotBytes := uint64(0)
	for key, expectedValue := range expectedValues {
		value, ok, hot, err = table.CacheAwareGet([]byte(key), true)
		require.NoError(t, err)
		require.True(t, ok)

		if hot {
			require.Equal(t, expectedValue, value)
			hotBytes += uint64(len(key)) + uint64(len(value))
		} else {
			require.Nil(t, value)
		}
	}
	require.LessOrEqual(t, hotBytes, maxCacheSize)

	// Read enough values to guarantee that the write cache is at full capacity.
	for key, expectedValue := range expectedValues {
		value, ok, hot, err = table.CacheAwareGet([]byte(key), false)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedValue, value)

		// Reading a cold value twice in a row should not cause it to become hot.
		if !hot {
			value, ok, hot, err = table.CacheAwareGet([]byte(key), false)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, expectedValue, value)
			require.True(t, hot)
		}
	}

	// Do a final scan of the values in the DB. The number of hot bytes should not exceed the sizes of the caches.
	maxCacheSize = config.WriteCacheSize + config.ReadCacheSize
	hotBytes = uint64(0)
	for key, expectedValue := range expectedValues {
		value, ok, hot, err = table.CacheAwareGet([]byte(key), true)
		require.NoError(t, err)
		require.True(t, ok)

		if hot {
			require.Equal(t, expectedValue, value)
			hotBytes += uint64(len(key)) + uint64(len(value))
		} else {
			require.Nil(t, value)
		}
	}
	require.LessOrEqual(t, hotBytes, maxCacheSize)

	err = db.Destroy()
	require.NoError(t, err)

	// ensure that the test directory is empty
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}
