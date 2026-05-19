package segment

import (
	"log/slog"
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// valueLocation pairs the (offset, length) of a written value so callers can later read it back.
// The value file no longer stores a length prefix, so callers must remember the length themselves
// (in production code, the length lives in the key file's Address record).
type valueLocation struct {
	offset uint32
	length uint32
}

func TestWriteThenReadValues(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	index := rand.Uint32()
	shard := uint8(rand.Uint32())
	valueCount := rand.Int32Range(100, 200)
	values := make([][]byte, valueCount)
	expectedFileSize := uint64(0)
	for i := 0; i < int(valueCount); i++ {
		values[i] = rand.VariableBytes(1, 100)
		expectedFileSize += uint64(len(values[i]))
	}

	// A map from the location of the value to the value itself.
	addressMap := make(map[valueLocation][]byte)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createValueFile(logger, index, shard, segmentPath, false)
	require.NoError(t, err)

	for _, value := range values {
		address, err := file.write(value)
		require.NoError(t, err)
		loc := valueLocation{offset: address, length: uint32(len(value))} //nolint:gosec // bounded
		addressMap[loc] = value

		// Occasionally flush the file to disk.
		if rand.BoolWithProbability(0.25) {
			err := file.flush()
			require.NoError(t, err)
		}

		// Occasionally scan all addresses and values in the file.
		if rand.BoolWithProbability(0.1) {
			err = file.flush()
			require.NoError(t, err)
			for loc, val := range addressMap {
				readValue, err := file.read(loc.offset, loc.length)
				require.NoError(t, err)
				require.Equal(t, val, readValue)
			}
		}
	}

	// Seal the file and read all values.
	err = file.seal()
	require.NoError(t, err)
	for loc, val := range addressMap {
		readValue, err := file.read(loc.offset, loc.length)
		require.NoError(t, err)
		require.Equal(t, val, readValue)
	}

	reportedFileSize := file.size
	stat, err := os.Stat(file.path())
	require.NoError(t, err)
	actualFileSize := uint64(stat.Size())
	require.Equal(t, actualFileSize, reportedFileSize)
	require.Equal(t, expectedFileSize, reportedFileSize)

	// Create a new in-memory instance from the on-disk file and verify that it behaves the same.
	file2, err := loadValueFile(logger, index, shard, []*SegmentPath{segmentPath})
	require.NoError(t, err)
	require.Equal(t, file.size, file2.size)
	for loc, val := range addressMap {
		readValue, err := file2.read(loc.offset, loc.length)
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
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	index := rand.Uint32()
	shard := uint8(rand.Uint32())
	valueCount := rand.Int32Range(100, 200)
	values := make([][]byte, valueCount)
	for i := 0; i < int(valueCount); i++ {
		values[i] = rand.VariableBytes(1, 100)
	}

	// A map from the location of the value to the value itself.
	addressMap := make(map[valueLocation][]byte)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createValueFile(logger, index, shard, segmentPath, false)
	require.NoError(t, err)

	var lastLoc valueLocation
	for _, value := range values {
		address, err := file.write(value)
		require.NoError(t, err)
		loc := valueLocation{offset: address, length: uint32(len(value))} //nolint:gosec // bounded
		addressMap[loc] = value
		lastLoc = loc
	}

	err = file.seal()
	require.NoError(t, err)

	// Truncate the file by chopping off some bytes from the end of the last value. Without the
	// length prefix in the file, every byte we cut off is value data, so reads of the last value
	// must fail and every other value must still read back correctly.
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
	for loc, val := range addressMap {
		if loc == lastLoc {
			_, err := file.read(loc.offset, loc.length)
			require.Error(t, err)
		} else {
			readValue, err := file.read(loc.offset, loc.length)
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
