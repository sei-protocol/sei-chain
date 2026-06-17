package segment

import (
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

const (

	// MetadataFileExtension is the file extension for the metadata file.
	MetadataFileExtension = ".metadata"

	// MetadataSwapExtension is the file extension for the metadata swap file. This file is used to atomically update
	// the metadata file by doing an atomic rename of the swap file to the metadata file. If this file is ever
	// present when the database first starts, it is an artifact of a crash during a metadata update, and should be
	// deleted.
	MetadataSwapExtension = MetadataFileExtension + util.SwapFileExtension

	// V3MetadataSize is the size of the metadata file at the current LatestSegmentVersion (the name
	// is kept for backwards compatibility; the metadata layout has not actually changed since v3).
	// Layout:
	//   - 4 bytes for version
	//   - 1 byte for the sharding factor
	//   - 8 bytes for lastValueTimestamp
	//   - 4 bytes for keyCount
	//   - 1 byte for sealed
	V3MetadataSize = 18
)

// metadataFile contains metadata about a segment. This file contains metadata about the data segment, such as
// serialization version and the lastValueTimestamp when the file was sealed.
type metadataFile struct {
	// The segment index. This value is encoded in the file name.
	index uint32

	// The serialization version for this segment, used to permit smooth data migrations.
	// This value is encoded in the file.
	segmentVersion SegmentVersion

	// The sharding factor for this segment. This value is encoded in the file.
	shardingFactor uint8

	// The time when the last value was written into the segment, in nanoseconds since the epoch. A segment can
	// only be deleted when all values within it are expired, and so we only need to keep track of the
	// lastValueTimestamp of the last value (which always expires last). This value is irrelevant if the segment is
	// not yet sealed. This value is encoded in the file.
	lastValueTimestamp uint64

	// The number of keys in the segment. This value is undefined if the segment is not yet sealed.
	// This value is encoded in the file.
	keyCount uint32

	// If true, the segment is sealed and no more data can be written to it. If false, then data can still be written
	// to this segment. This value is encoded in the file.
	sealed bool

	// Path data for the segment file. This information is not serialized in the metadata file.
	segmentPath *SegmentPath

	// If true, then use fsync to make metadata updates atomic. Should always be true in production, but can be
	// set to false in tests to speed up unit tests. Not serialized to the file.
	fsync bool
}

// createMetadataFile creates a new metadata file. When this method returns, the metadata file will
// be durably written to disk.
func createMetadataFile(
	index uint32,
	shardingFactor uint8,
	path *SegmentPath,
	fsync bool,
) (*metadataFile, error) {

	file := &metadataFile{
		index:       index,
		segmentPath: path,
		fsync:       fsync,
	}

	file.segmentVersion = LatestSegmentVersion
	file.shardingFactor = shardingFactor
	err := file.write()
	if err != nil {
		return nil, fmt.Errorf("failed to write metadata file: %v", err)
	}

	return file, nil
}

// loadMetadataFile loads the metadata file from disk, looking in the given parent directories until it finds the file.
// If the file is not found, it returns an error.
func loadMetadataFile(index uint32, segmentPaths []*SegmentPath, fsync bool) (*metadataFile, error) {
	metadataFileName := fmt.Sprintf("%d%s", index, MetadataFileExtension)
	metadataPath, err := lookForFile(segmentPaths, metadataFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to find metadata file: %w", err)
	}
	if metadataPath == nil {
		return nil, fmt.Errorf("failed to find metadata file %s", metadataFileName)
	}

	file := &metadataFile{
		index:       index,
		segmentPath: metadataPath,
		fsync:       fsync,
	}

	filePath := file.path()

	data, err := os.ReadFile(filePath) //nolint:gosec // path within segment directory
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file %s: %v", filePath, err)
	}
	err = file.deserialize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize metadata file %s: %v", filePath, err)
	}

	return file, nil
}

// MetadataFileExtension is the file extension for the metadata file. Metadata file names have the form "X.metadata",
// where X is the segment index.
func getMetadataFileIndex(fileName string) (uint32, error) {
	indexString := path.Base(fileName)[:len(fileName)-len(MetadataFileExtension)]
	index, err := strconv.Atoi(indexString)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index from file name %s: %v", fileName, err)
	}

	return uint32(index), nil //nolint:gosec // segment index fits uint32
}

// Size returns the size of the metadata file in bytes.
func (m *metadataFile) Size() uint64 {
	return V3MetadataSize
}

// Name returns the file name for this metadata file.
func (m *metadataFile) name() string {
	return fmt.Sprintf("%d%s", m.index, MetadataFileExtension)
}

// Path returns the full path to this metadata file.
func (m *metadataFile) path() string {
	return path.Join(m.segmentPath.SegmentDirectory(), m.name())
}

// Seal seals the segment. This action will atomically write the metadata file to disk one final time,
// and should only be performed when all data that will be written to the key/value files has been made durable.
func (m *metadataFile) seal(now time.Time, keyCount uint32) error {
	m.sealed = true
	m.lastValueTimestamp = uint64(now.UnixNano()) //nolint:gosec // wall-clock nanos non-negative
	m.keyCount = keyCount
	err := m.write()
	if err != nil {
		return fmt.Errorf("failed to write sealed metadata file: %v", err)
	}
	return nil
}

// serialize serializes the metadata file to a byte array.
func (m *metadataFile) serialize() []byte {
	data := make([]byte, V3MetadataSize)

	binary.BigEndian.PutUint32(data[0:4], uint32(m.segmentVersion))
	data[4] = m.shardingFactor
	binary.BigEndian.PutUint64(data[5:13], m.lastValueTimestamp)
	binary.BigEndian.PutUint32(data[13:17], m.keyCount)
	if m.sealed {
		data[17] = 1
	} else {
		data[17] = 0
	}

	return data
}

// deserialize deserializes the metadata file from a byte array.
func (m *metadataFile) deserialize(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("metadata file is not the correct size, expected at least 4 bytes, got %d", len(data))
	}

	m.segmentVersion = SegmentVersion(binary.BigEndian.Uint32(data[0:4]))
	if m.segmentVersion != LatestSegmentVersion {
		return fmt.Errorf("unsupported segment version: %d (only version %d is supported)",
			m.segmentVersion, LatestSegmentVersion)
	}

	if len(data) != V3MetadataSize {
		return fmt.Errorf("metadata file is not the correct size, expected %d, got %d",
			V3MetadataSize, len(data))
	}

	m.shardingFactor = data[4]
	m.lastValueTimestamp = binary.BigEndian.Uint64(data[5:13])
	m.keyCount = binary.BigEndian.Uint32(data[13:17])
	m.sealed = data[17] == 1

	return nil
}

// write atomically writes the metadata file to disk.
func (m *metadataFile) write() error {
	err := util.AtomicWrite(m.path(), m.serialize(), m.fsync)
	if err != nil {
		return fmt.Errorf("failed to write metadata file %s: %v", m.path(), err)
	}

	return nil
}

// snapshot creates a hard link to the file in the snapshot directory, and a soft link to the hard linked file in the
// soft link directory. Requires that the file is sealed and that snapshotting is enabled.
func (m *metadataFile) snapshot() error {
	if !m.sealed {
		return fmt.Errorf("file %s is not sealed, cannot take Snapshot", m.path())
	}

	err := m.segmentPath.Snapshot(m.name())
	if err != nil {
		return fmt.Errorf("failed to create Snapshot: %v", err)
	}

	return nil
}

// delete deletes the metadata file from disk. If the file is a snapshot (i.e., a symlink), this method will also
// delete the actual file that the symlink points to.
func (m *metadataFile) delete() error {
	err := util.DeepDelete(m.path())
	if err != nil {
		return fmt.Errorf("failed to delete metadata file %s: %w", m.path(), err)
	}
	return nil
}
