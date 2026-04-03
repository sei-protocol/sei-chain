package segment

import (
	"os"
	"testing"

	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadWriteKeys(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	logger := test.GetLogger()
	directory := t.TempDir()

	index := rand.Uint32()

	keyCount := rand.Int32Range(100, 200)
	keys := make([]*types.ScopedKey, keyCount)
	for i := 0; i < int(keyCount); i++ {
		key := rand.VariableBytes(1, 100)
		address := types.Address(rand.Uint64())
		valueSize := rand.Uint32()
		keys[i] = &types.ScopedKey{Key: key, Address: address, ValueSize: valueSize}
	}

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createKeyFile(logger, index, segmentPath, false)
	require.NoError(t, err)

	for _, key := range keys {
		err := file.write(key)
		require.NoError(t, err)
	}

	// Reading the file prior to sealing it is forbidden.
	_, err = file.readKeys()
	require.Error(t, err)

	err = file.seal()
	require.NoError(t, err)

	// Verify that file size is correctly reported.
	reportedSize := file.Size()
	stat, err := os.Stat(file.path())
	require.NoError(t, err)
	actualSize := uint64(stat.Size())
	require.Equal(t, actualSize, reportedSize)

	// Reading the file after sealing it is allowed.
	readKeys, err := file.readKeys()
	require.NoError(t, err)

	for i, key := range keys {
		assert.Equal(t, key, readKeys[i])
	}

	// Create a new in-memory instance from the on-disk file and verify that it behaves the same.
	file2, err := loadKeyFile(logger, index, []*SegmentPath{segmentPath}, ValueSizeSegmentVersion)
	require.NoError(t, err)
	require.Equal(t, file.Size(), file2.Size())

	readKeys, err = file2.readKeys()
	require.NoError(t, err)
	for i, key := range keys {
		assert.Equal(t, key, readKeys[i])
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

func TestReadingTruncatedKeyFile(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	logger := test.GetLogger()
	directory := t.TempDir()

	index := rand.Uint32()

	keyCount := rand.Int32Range(100, 200)
	keys := make([]*types.ScopedKey, keyCount)
	for i := 0; i < int(keyCount); i++ {
		key := rand.VariableBytes(1, 100)
		address := types.Address(rand.Uint64())
		valueSize := rand.Uint32()
		keys[i] = &types.ScopedKey{Key: key, Address: address, ValueSize: valueSize}
	}

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createKeyFile(logger, index, segmentPath, false)
	require.NoError(t, err)

	for _, key := range keys {
		err := file.write(key)
		require.NoError(t, err)
	}

	err = file.seal()
	require.NoError(t, err)

	// Truncate the file. Chop off some bytes from the last key, but do not corrupt the length prefix.
	lastKeyLength := len(keys[keyCount-1].Key)

	filePath := file.path()

	originalBytes, err := os.ReadFile(filePath)
	require.NoError(t, err)

	bytesToRemove := rand.Int32Range(1, int32(lastKeyLength)+1)
	bytes := originalBytes[:len(originalBytes)-int(bytesToRemove)]

	err = os.WriteFile(filePath, bytes, 0644)
	require.NoError(t, err)

	// We should be able to read the keys up to the point where the file was truncated.
	readKeys, err := file.readKeys()
	require.NoError(t, err)

	require.Equal(t, int(keyCount-1), len(readKeys))
	for i, key := range keys[:keyCount-1] {
		assert.Equal(t, key, readKeys[i])
	}

	// Truncate the file. This time, chop off some of the last entry.
	prefixBytesToRemove := rand.Int32Range(1, 8)
	bytes = originalBytes[:len(originalBytes)-int(prefixBytesToRemove)]

	err = os.WriteFile(filePath, bytes, 0644)
	require.NoError(t, err)

	// We should not be able to read the keys if the length prefix is truncated.
	keys, err = file.readKeys()
	require.NoError(t, err)

	require.Equal(t, int(keyCount-1), len(keys))
	for i, key := range keys[:keyCount-1] {
		assert.Equal(t, key, keys[i])
	}

	// delete the file
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = file.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}

func TestSwappingKeyFile(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	logger := test.GetLogger()
	directory := t.TempDir()

	index := rand.Uint32()

	keyCount := rand.Int32Range(100, 200)
	keys := make([]*types.ScopedKey, keyCount)
	for i := 0; i < int(keyCount); i++ {
		key := rand.VariableBytes(1, 100)
		address := types.Address(rand.Uint64())
		valueSize := rand.Uint32()
		keys[i] = &types.ScopedKey{Key: key, Address: address, ValueSize: valueSize}
	}

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	file, err := createKeyFile(logger, index, segmentPath, false)
	require.NoError(t, err)

	for _, key := range keys {
		err := file.write(key)
		require.NoError(t, err)
	}

	// Reading the file prior to sealing it is forbidden.
	_, err = file.readKeys()
	require.Error(t, err)

	err = file.seal()
	require.NoError(t, err)

	// Verify that file size is correctly reported.
	reportedSize := file.Size()
	stat, err := os.Stat(file.path())
	require.NoError(t, err)
	actualSize := uint64(stat.Size())
	require.Equal(t, actualSize, reportedSize)

	// Reading the file after sealing it is allowed.
	readKeys, err := file.readKeys()
	require.NoError(t, err)

	for i, key := range keys {
		assert.Equal(t, key, readKeys[i])
	}

	// Create a new in-memory instance from the on-disk file and verify that it behaves the same.
	file2, err := loadKeyFile(logger, index, []*SegmentPath{segmentPath}, ValueSizeSegmentVersion)
	require.NoError(t, err)
	require.Equal(t, file.Size(), file2.Size())

	readKeys, err = file2.readKeys()
	require.NoError(t, err)
	for i, key := range keys {
		assert.Equal(t, key, readKeys[i])
	}

	// Create a new version of the key file that only contains the keys at even indices. The intention is to replace
	// the on-disk file with this new version.
	swapFile, err := createKeyFile(logger, index, segmentPath, true)
	require.NoError(t, err)
	for i := 0; i < int(keyCount); i += 2 {
		err := swapFile.write(keys[i])
		require.NoError(t, err)
	}
	err = swapFile.seal()
	require.NoError(t, err)

	// Verify that the swap file is present on disk.
	swapFilePath := swapFile.path()
	_, err = os.Stat(swapFilePath)
	require.NoError(t, err)

	// The swap file path should be different from the original file path.
	originalFilePath := file.path()
	require.NotEqual(t, swapFilePath, originalFilePath)

	// Replace the old file with the new one.
	err = swapFile.atomicSwap(false)
	require.NoError(t, err)

	// The old swap file should no longer be present.
	_, err = os.Stat(swapFilePath)
	require.True(t, os.IsNotExist(err))

	// The "regular" file should still be present.
	_, err = os.Stat(originalFilePath)
	require.NoError(t, err)

	// Verify that the file size is correctly reported after the swap.
	reportedSize = swapFile.Size()
	stat, err = os.Stat(swapFile.path())
	require.NoError(t, err)
	actualSize = uint64(stat.Size())
	require.Equal(t, actualSize, reportedSize)

	// Verify the contents of the new file. Reload it from disk just to ensure that we aren't "cheating" somehow.
	file2, err = loadKeyFile(logger, index, []*SegmentPath{segmentPath}, ValueSizeSegmentVersion)
	require.NoError(t, err)
	readKeys, err = file2.readKeys()
	require.NoError(t, err)
	for i, key := range keys {
		if i%2 == 0 {
			assert.Equal(t, key, readKeys[i/2])
		}
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
