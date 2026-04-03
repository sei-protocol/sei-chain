package segment

import (
	"os"
	"testing"

	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestWriteThenReadValues(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	logger := test.GetLogger()
	directory := t.TempDir()

	index := rand.Uint32()
	shard := rand.Uint32()
	valueCount := rand.Int32Range(100, 200)
	values := make([][]byte, valueCount)
	expectedFileSize := uint64(0)
	for i := 0; i < int(valueCount); i++ {
		values[i] = rand.VariableBytes(1, 100)
		expectedFileSize += uint64(len(values[i])) + 4 /* length uint32 */
	}

	// A map from the first byte index of the value to the value itself.
	addressMap := make(map[uint32][]byte)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createValueFile(logger, index, shard, segmentPath, false)
	require.NoError(t, err)

	for _, value := range values {
		address, err := file.write(value)
		require.NoError(t, err)
		addressMap[address] = value

		// Occasionally flush the file to disk.
		if rand.BoolWithProbability(0.25) {
			err := file.flush()
			require.NoError(t, err)
		}

		// Occasionally scan all addresses and values in the file.
		if rand.BoolWithProbability(0.1) {
			err = file.flush()
			require.NoError(t, err)
			for key, val := range addressMap {
				readValue, err := file.read(key)
				require.NoError(t, err)
				require.Equal(t, val, readValue)
			}
		}
	}

	// Seal the file and read all values.
	err = file.seal()
	require.NoError(t, err)
	for key, val := range addressMap {
		readValue, err := file.read(key)
		require.NoError(t, err)
		require.Equal(t, val, readValue)
	}

	reportedFileSize := file.size
	stat, err := os.Stat(file.path())
	require.NoError(t, err)
	actualFileSize := uint64(stat.Size())
	require.Equal(t, actualFileSize, reportedFileSize)

	// Create a new in-memory instance from the on-disk file and verify that it behaves the same.
	file2, err := loadValueFile(logger, index, shard, []*SegmentPath{segmentPath})
	require.NoError(t, err)
	require.Equal(t, file.size, file2.size)
	for key, val := range addressMap {
		readValue, err := file2.read(key)
		require.NoError(t, err)
		require.Equal(t, val, readValue)
	}

	// delete the file
	filePath := file.path()
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = file.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}

func TestReadingTruncatedValueFile(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	logger := test.GetLogger()
	directory := t.TempDir()

	index := rand.Uint32()
	shard := rand.Uint32()
	valueCount := rand.Int32Range(100, 200)
	values := make([][]byte, valueCount)
	for i := 0; i < int(valueCount); i++ {
		values[i] = rand.VariableBytes(1, 100)
	}

	// A map from the first byte index of the value to the value itself.
	addressMap := make(map[uint32][]byte)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createValueFile(logger, index, shard, segmentPath, false)
	require.NoError(t, err)

	var lastAddress uint32
	for _, value := range values {
		address, err := file.write(value)
		require.NoError(t, err)
		addressMap[address] = value
		lastAddress = address
	}

	err = file.seal()
	require.NoError(t, err)

	// Truncate the file. Chop off some bytes from the last value, but do not corrupt the length prefix.
	lastValueLength := len(values[valueCount-1])

	filePath := file.path()

	originalBytes, err := os.ReadFile(filePath)
	require.NoError(t, err)

	bytesToRemove := rand.Int32Range(1, int32(lastValueLength)+1)
	bytes := originalBytes[:len(originalBytes)-int(bytesToRemove)]

	err = os.WriteFile(filePath, bytes, 0644)
	require.NoError(t, err)

	file, err = loadValueFile(logger, index, shard, []*SegmentPath{segmentPath})
	require.NoError(t, err)

	// We should be able to read all values except for the last one.
	for key, val := range addressMap {
		if key == lastAddress {
			_, err := file.read(key)
			require.Error(t, err)
		} else {
			readValue, err := file.read(key)
			require.NoError(t, err)
			require.Equal(t, val, readValue)
		}
	}

	// Truncate the file. Corrupt the length prefix of the last value.
	prefixBytesToRemove := rand.Int32Range(1, 4)
	bytes = originalBytes[:len(originalBytes)-int(prefixBytesToRemove)]

	err = os.WriteFile(filePath, bytes, 0644)
	require.NoError(t, err)

	file, err = loadValueFile(logger, index, shard, []*SegmentPath{segmentPath})
	require.NoError(t, err)

	// We should be able to read all values except for the last one.
	for key, val := range addressMap {
		if key == lastAddress {
			_, err := file.read(key)
			require.Error(t, err)
		} else {
			readValue, err := file.read(key)
			require.NoError(t, err)
			require.Equal(t, val, readValue)
		}
	}

	// delete the file
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = file.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}
