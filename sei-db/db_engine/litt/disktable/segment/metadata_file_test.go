package segment

import (
	"os"
	"testing"

	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestUnsealedSerialization(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	shardingFactor := rand.Uint32()
	salt := ([16]byte)(rand.Bytes(16))
	timestamp := rand.Uint64()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m := &metadataFile{
		index:              index,
		segmentVersion:     LatestSegmentVersion,
		shardingFactor:     shardingFactor,
		salt:               salt,
		lastValueTimestamp: timestamp,
		sealed:             false,
		segmentPath:        segmentPath,
	}
	err = m.write()
	require.NoError(t, err)

	deserialized, err := loadMetadataFile(index, []*SegmentPath{segmentPath}, false)
	require.NoError(t, err)
	require.Equal(t, *m, *deserialized)

	reportedSize := m.Size()
	stat, err := os.Stat(m.path())
	require.NoError(t, err)
	actualSize := uint64(stat.Size())
	require.Equal(t, actualSize, reportedSize)

	// delete the file
	filePath := m.path()
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = m.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}

func TestSealedSerialization(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	shardingFactor := rand.Uint32()
	salt := ([16]byte)(rand.Bytes(16))
	timestamp := rand.Uint64()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m := &metadataFile{
		index:              index,
		segmentVersion:     LatestSegmentVersion,
		shardingFactor:     shardingFactor,
		salt:               salt,
		lastValueTimestamp: timestamp,
		sealed:             true,
		segmentPath:        segmentPath,
	}
	err = m.write()
	require.NoError(t, err)

	reportedSize := m.Size()
	stat, err := os.Stat(m.path())
	require.NoError(t, err)
	actualSize := uint64(stat.Size())
	require.Equal(t, actualSize, reportedSize)

	deserialized, err := loadMetadataFile(index, []*SegmentPath{segmentPath}, false)
	require.NoError(t, err)
	require.Equal(t, *m, *deserialized)

	// delete the file
	filePath := m.path()
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = m.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}

func TestFreshFileSerialization(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	salt := ([16]byte)(rand.Bytes(16))

	index := rand.Uint32()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m, err := createMetadataFile(index, 1234, salt, segmentPath, false)
	require.NoError(t, err)

	require.Equal(t, index, m.index)
	require.Equal(t, LatestSegmentVersion, m.segmentVersion)
	require.False(t, m.sealed)
	require.Zero(t, m.lastValueTimestamp)

	reportedSize := m.Size()
	stat, err := os.Stat(m.path())
	require.NoError(t, err)
	actualSize := uint64(stat.Size())
	require.Equal(t, actualSize, reportedSize)

	deserialized, err := loadMetadataFile(index, []*SegmentPath{segmentPath}, false)
	require.NoError(t, err)
	require.Equal(t, *m, *deserialized)

	// delete the file
	filePath := m.path()
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = m.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}

func TestSealing(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	directory := t.TempDir()

	salt := ([16]byte)(rand.Bytes(16))

	index := rand.Uint32()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m, err := createMetadataFile(index, 1234, salt, segmentPath, false)
	require.NoError(t, err)

	// seal the file
	sealTime := rand.Time()
	err = m.seal(sealTime, 987)
	require.NoError(t, err)

	require.Equal(t, index, m.index)
	require.Equal(t, LatestSegmentVersion, m.segmentVersion)
	require.True(t, m.sealed)
	require.Equal(t, uint64(sealTime.UnixNano()), m.lastValueTimestamp)
	require.Equal(t, salt, m.salt)
	require.Equal(t, uint32(1234), m.shardingFactor)
	require.Equal(t, uint32(987), m.keyCount)

	// load the file
	deserialized, err := loadMetadataFile(index, []*SegmentPath{segmentPath}, false)
	require.NoError(t, err)
	require.Equal(t, *m, *deserialized)

	// delete the file
	filePath := m.path()
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	err = m.delete()
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}
