package segment

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

func TestUnsealedSerialization(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	shardingFactor := uint8(rand.Uint32Range(1, 256))
	timestamp := rand.Uint64()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m := &metadataFile{
		index:              index,
		segmentVersion:     LatestSegmentVersion,
		shardingFactor:     shardingFactor,
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

func TestCompressionAlgorithmSerialization(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)

	m, err := createMetadataFile(index, 4, types.CompressionS2, segmentPath, false)
	require.NoError(t, err)
	require.Equal(t, types.CompressionS2, m.compressionAlgorithm)
	require.Equal(t, LatestSegmentVersion, m.segmentVersion)

	// The on-disk file is the v4 size, and the algorithm survives a round trip.
	stat, err := os.Stat(m.path())
	require.NoError(t, err)
	require.Equal(t, int64(V4MetadataSize), stat.Size())

	deserialized, err := loadMetadataFile(index, []*SegmentPath{segmentPath}, false)
	require.NoError(t, err)
	require.Equal(t, types.CompressionS2, deserialized.compressionAlgorithm)
	require.Equal(t, *m, *deserialized)
}

// TestV3MetadataReadsAsUncompressed verifies that a legacy version-3 metadata file (which has no
// compression byte) still loads, and is interpreted as CompressionNone.
func TestV3MetadataReadsAsUncompressed(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)

	// Write a v4 file, then rewrite it in the legacy v3 layout: version 3 and no trailing compression
	// byte (18 bytes instead of 19).
	m, err := createMetadataFile(index, 7, types.CompressionNone, segmentPath, false)
	require.NoError(t, err)

	data, err := os.ReadFile(m.path())
	require.NoError(t, err)
	require.Equal(t, V4MetadataSize, len(data))
	v3 := data[:V3MetadataSize]
	binary.BigEndian.PutUint32(v3[0:4], uint32(ShardedAddressSegmentVersion))
	require.NoError(t, os.WriteFile(m.path(), v3, 0600))

	deserialized, err := loadMetadataFile(index, []*SegmentPath{segmentPath}, false)
	require.NoError(t, err)
	require.Equal(t, ShardedAddressSegmentVersion, deserialized.segmentVersion)
	require.Equal(t, types.CompressionNone, deserialized.compressionAlgorithm)
	require.EqualValues(t, 7, deserialized.shardingFactor)
}

func TestSealedSerialization(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	shardingFactor := uint8(rand.Uint32Range(1, 256))
	timestamp := rand.Uint64()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m := &metadataFile{
		index:              index,
		segmentVersion:     LatestSegmentVersion,
		shardingFactor:     shardingFactor,
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
	rand := util.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m, err := createMetadataFile(index, 123, types.CompressionNone, segmentPath, false)
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
	rand := util.NewTestRandom()
	directory := t.TempDir()

	index := rand.Uint32()
	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	m, err := createMetadataFile(index, 123, types.CompressionNone, segmentPath, false)
	require.NoError(t, err)

	// seal the file
	sealTime := rand.Time()
	err = m.seal(sealTime, 987)
	require.NoError(t, err)

	require.Equal(t, index, m.index)
	require.Equal(t, LatestSegmentVersion, m.segmentVersion)
	require.True(t, m.sealed)
	require.Equal(t, uint64(sealTime.UnixNano()), m.lastValueTimestamp)
	require.Equal(t, uint8(123), m.shardingFactor)
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
