package benchmark

import (
	"os"
	"testing"
	"time"

	config2 "github.com/Layr-Labs/eigenda/litt/benchmark/config"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/docker/go-units"
	"github.com/stretchr/testify/require"
)

func TestTrackerDeterminism(t *testing.T) {
	ctx := t.Context()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	config := config2.DefaultBenchmarkConfig()
	config.RandomPoolSize = units.MiB
	config.CohortSize = rand.Uint64Range(10, 20)
	config.MetadataDirectory = directory
	config.Seed = rand.Int63()
	config.ValueSizeMB = 1.0 / 1024 // 1kb
	config.TTLHours = 1

	// Generate enough data to fill 10ish cohorts.
	keyCount := 10*config.CohortSize + rand.Uint64Range(0, 10)

	errorMonitor := util.NewErrorMonitor(ctx, config.LittConfig.Logger, nil)

	dataTracker, err := NewDataTracker(ctx, config, errorMonitor)
	require.NoError(t, err)

	// map from indices to keys
	expectedKeys := make(map[uint64][]byte)

	// map from indices to values
	expectedValues := make(map[uint64][]byte)

	// Get a bunch of values.
	for i := uint64(0); i < keyCount; i++ {
		writeInfo := dataTracker.GetWriteInfo()
		require.Equal(t, i, writeInfo.KeyIndex)
		require.Equal(t, 32, len(writeInfo.Key))
		require.Equal(t, units.KiB, len(writeInfo.Value))

		expectedKeys[i] = writeInfo.Key
		expectedValues[i] = writeInfo.Value
	}

	dataTracker.Close()

	// Rebuild the tracker at genesis. We should get the same sequence of keys and values.
	err = os.RemoveAll(directory)
	require.NoError(t, err)
	err = os.MkdirAll(directory, os.ModePerm)
	require.NoError(t, err)
	dataTracker, err = NewDataTracker(ctx, config, errorMonitor)
	require.NoError(t, err)

	for i := uint64(0); i < keyCount; i++ {
		writeInfo := dataTracker.GetWriteInfo()
		require.Equal(t, i, writeInfo.KeyIndex)
		require.Equal(t, 32, len(writeInfo.Key))
		require.Equal(t, units.KiB, len(writeInfo.Value))
		require.Equal(t, expectedKeys[i], writeInfo.Key)
		require.Equal(t, expectedValues[i], writeInfo.Value)
	}

	dataTracker.Close()

	err = os.RemoveAll(directory)
	require.NoError(t, err)
	ok, _ := errorMonitor.IsOk()
	require.True(t, ok)
}

func TestTrackerRestart(t *testing.T) {
	ctx := t.Context()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	config := config2.DefaultBenchmarkConfig()
	config.RandomPoolSize = units.MiB
	config.CohortSize = rand.Uint64Range(10, 20)
	config.MetadataDirectory = directory
	config.Seed = rand.Int63()
	config.ValueSizeMB = 1.0 / 1024 // 1kb

	// Generate enough data to fill 10ish cohorts.
	keyCount := 10*config.CohortSize + rand.Uint64Range(0, 10)

	errorMonitor := util.NewErrorMonitor(ctx, config.LittConfig.Logger, nil)

	dataTracker, err := NewDataTracker(ctx, config, errorMonitor)
	require.NoError(t, err)

	indexSet := make(map[uint64]struct{})

	// Generate a bunch of values.
	for i := uint64(0); i < keyCount; i++ {
		writeInfo := dataTracker.GetWriteInfo()
		require.Equal(t, i, writeInfo.KeyIndex)
		require.Equal(t, 32, len(writeInfo.Key))
		require.Equal(t, units.KiB, len(writeInfo.Value))

		indexSet[writeInfo.KeyIndex] = struct{}{}
	}

	// All indices should be unique.
	require.Equal(t, keyCount, uint64(len(indexSet)))

	// Restart.
	dataTracker.Close()
	dataTracker, err = NewDataTracker(ctx, config, errorMonitor)
	require.NoError(t, err)

	// Generate more values.
	for i := uint64(0); i < keyCount; i++ {
		writeInfo := dataTracker.GetWriteInfo()
		indexSet[writeInfo.KeyIndex] = struct{}{}
	}

	// If we aren't reusing indices after the restart, then the set should now be equal to 2*keyCount.
	require.Equal(t, 2*keyCount, uint64(len(indexSet)))

	dataTracker.Close()

	err = os.RemoveAll(directory)
	require.NoError(t, err)

	ok, _ := errorMonitor.IsOk()
	require.True(t, ok)
}

func TestTrackReads(t *testing.T) {
	ctx := t.Context()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	config := config2.DefaultBenchmarkConfig()
	config.RandomPoolSize = units.MiB
	config.CohortSize = rand.Uint64Range(10, 20)
	config.MetadataDirectory = directory
	config.Seed = rand.Int63()
	config.ValueSizeMB = 1.0 / 1024 // 1kb

	// Generate enough data to fill exactly 10 cohorts.
	keyCount := 10 * config.CohortSize

	errorMonitor := util.NewErrorMonitor(ctx, config.LittConfig.Logger, nil)

	dataTracker, err := NewDataTracker(ctx, config, errorMonitor)
	require.NoError(t, err)

	keyToIndexMap := make(map[string]uint64)

	// When reading, we should only ever read from indices that have been confirmed written.
	highestWrittenIndex := -1
	highestIndexReportedWritten := -1
	readCount := uint64(0)

	// Generate a bunch of values.
	for i := uint64(0); i < keyCount; i++ {
		writeInfo := dataTracker.GetWriteInfo()
		require.Equal(t, i, writeInfo.KeyIndex)
		require.Equal(t, 32, len(writeInfo.Key))
		require.Equal(t, units.KiB, len(writeInfo.Value))

		keyToIndexMap[string(writeInfo.Key)] = writeInfo.KeyIndex

		if rand.Float64() < 0.1 && i > 2*config.CohortSize {
			// Advance the highest written index.
			possibleIndex := rand.Uint64Range(i-config.CohortSize*2, i)
			if int(possibleIndex) > highestWrittenIndex {
				highestWrittenIndex = int(possibleIndex)
			} else {
				highestWrittenIndex++
			}
			for highestIndexReportedWritten < highestWrittenIndex {
				highestIndexReportedWritten++
				dataTracker.ReportWrite(uint64(highestIndexReportedWritten))
			}

			// Give the data tracker time to ingest data. Not required for the test to pass.
			time.Sleep(10 * time.Millisecond)
		}

		// Read a random value.
		var readInfo *ReadInfo
		if readCount == 0 {
			// We are reading the first value, so one might not be available yet. Don't block forever.
			readInfo = dataTracker.GetReadInfoWithTimeout(time.Millisecond)
		} else {
			// After we read the first value, we should never block.
			readInfo = dataTracker.GetReadInfo()
		}
		if readInfo != nil {
			readCount++
			index := keyToIndexMap[string(readInfo.Key)]

			// we should not read values we haven't told the data tracker we've written.
			require.True(t, int(index) <= highestWrittenIndex)
		}
	}

	require.True(t, readCount > 0)

	// Mark all data as having been written so far.
	highestWrittenIndex = int(keyCount - 1)
	for highestIndexReportedWritten < highestWrittenIndex {
		highestIndexReportedWritten++
		dataTracker.ReportWrite(uint64(highestIndexReportedWritten))
	}

	unwrittenKeys := make(map[string]struct{})

	// Write a bunch more data, but do not mark any of it as having been written.
	for i := uint64(0); i < keyCount; i++ {
		writeInfo := dataTracker.GetWriteInfo()
		unwrittenKeys[string(writeInfo.Key)] = struct{}{}
	}

	// Restart the tracker without marking any of the new data as having been written.
	dataTracker.Close()
	dataTracker, err = NewDataTracker(ctx, config, errorMonitor)
	require.NoError(t, err)

	// Read a bunch of data.
	readDataSet := make(map[string]struct{})
	for i := uint64(0); i < keyCount*10; i++ {
		readInfo := dataTracker.GetReadInfo()
		require.NotNil(t, readInfo)

		if _, ok := unwrittenKeys[string(readInfo.Key)]; ok {
			// We should not be able to read data that we haven't marked as having been written.
			require.Fail(t, "read unwritten data")
		}

		readDataSet[string(readInfo.Key)] = struct{}{}
	}

	// The data we read is random, but the following heuristic should hold with high probability.
	require.True(t, len(readDataSet) > int(0.5*float64(keyCount)))

	dataTracker.Close()

	err = os.RemoveAll(directory)
	require.NoError(t, err)
	ok, _ := errorMonitor.IsOk()
	require.True(t, ok)
}
