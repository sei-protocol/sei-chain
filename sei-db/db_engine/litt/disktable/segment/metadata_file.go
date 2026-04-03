package segment

import (
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/Layr-Labs/eigenda/litt/util"
)

const (

	// MetadataFileExtension is the file extension for the metadata file.
	MetadataFileExtension = ".metadata"

	// MetadataSwapExtension is the file extension for the metadata swap file. This file is used to atomically update
	// the metadata file by doing an atomic rename of the swap file to the metadata file. If this file is ever
	// present when the database first starts, it is an artifact of a crash during a metadata update, and should be
	// deleted.
	MetadataSwapExtension = MetadataFileExtension + util.SwapFileExtension

	// V0MetadataSize is the size the metadata file at version 0 (aka OldHashFunctionSegmentVersion)
	// This is a constant, so it's convenient to have it here.
	// - 4 bytes for version
	// - 4 bytes for the sharding factor
	// - 4 bytes for salt
	// - 8 bytes for lastValueTimestamp
	// - and 1 byte for sealed.
	V0MetadataSize = 21

	// V1MetadataSize is the size of the metadata file at version 1 (aka SipHashSegmentVersion).
	// This is a constant, so it's convenient to have it here.
	// - 4 bytes for version
	// - 4 bytes for the sharding factor
	// - 16 bytes for salt
	// - 8 bytes for lastValueTimestamp
	// - and 1 byte for sealed.
	V1MetadataSize = 33

	// V2MetadataSize is the size of the metadata file at version 2 (aka ValueSizeSegmentVersion).
	// This is a constant, so it's convenient to have it here.
	// - 4 bytes for version
	// - 4 bytes for the sharding factor
	// - 16 bytes for salt
	// - 8 bytes for lastValueTimestamp
	// - 4 bytes for keyCount
	// - and 1 byte for sealed.
	V2MetadataSize = 37
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
	shardingFactor uint32

	// A random number, used to make the sharding hash function hard for an attacker to predict.
	// This value is encoded in the file. Note: after the hash function change, this value is
	// only used for data written with the old hash function.
	legacySalt uint32

	// A random byte array, used to make the sharding hash function hard for an attacker to predict.
	// This value is encoded in the file.
	salt [16]byte

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
	shardingFactor uint32,
	salt [16]byte,
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
	file.salt = salt
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

	data, err := os.ReadFile(filePath)
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

	return uint32(index), nil
}

// Size returns the size of the metadata file in bytes.
func (m *metadataFile) Size() uint64 {
	switch m.segmentVersion {
	case OldHashFunctionSegmentVersion:
		return V0MetadataSize
	case SipHashSegmentVersion:
		return V1MetadataSize
	default:
		return V2MetadataSize
	}
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
	m.lastValueTimestamp = uint64(now.UnixNano())
	m.keyCount = keyCount
	err := m.write()
	if err != nil {
		return fmt.Errorf("failed to write sealed metadata file: %v", err)
	}
	return nil
}

func (m *metadataFile) serializeV0Legacy() []byte {
	data := make([]byte, V0MetadataSize)

	// Write the version
	binary.BigEndian.PutUint32(data[0:4], uint32(m.segmentVersion))

	// Write the sharding factor
	binary.BigEndian.PutUint32(data[4:8], m.shardingFactor)

	// Write the salt
	binary.BigEndian.PutUint32(data[8:12], m.legacySalt)

	// Write the lastValueTimestamp
	binary.BigEndian.PutUint64(data[12:20], m.lastValueTimestamp)

	// Write the sealed flag
	if m.sealed {
		data[20] = 1
	} else {
		data[20] = 0
	}

	return data
}

func (m *metadataFile) serializeV1Legacy() []byte {
	data := make([]byte, V1MetadataSize)

	// Write the version
	binary.BigEndian.PutUint32(data[0:4], uint32(m.segmentVersion))

	// Write the sharding factor
	binary.BigEndian.PutUint32(data[4:8], m.shardingFactor)

	// Write the salt
	copy(data[8:24], m.salt[:])

	// Write the lastValueTimestamp
	binary.BigEndian.PutUint64(data[24:32], m.lastValueTimestamp)

	// Write the sealed flag
	if m.sealed {
		data[32] = 1
	} else {
		data[32] = 0
	}

	return data
}

// serialize serializes the metadata file to a byte array.
func (m *metadataFile) serialize() []byte {
	if m.segmentVersion == OldHashFunctionSegmentVersion {
		return m.serializeV0Legacy()
	} else if m.segmentVersion == SipHashSegmentVersion {
		return m.serializeV1Legacy()
	}

	data := make([]byte, V2MetadataSize)

	// Write the version
	binary.BigEndian.PutUint32(data[0:4], uint32(m.segmentVersion))

	// Write the sharding factor
	binary.BigEndian.PutUint32(data[4:8], m.shardingFactor)

	// Write the salt
	copy(data[8:24], m.salt[:])

	// Write the lastValueTimestamp
	binary.BigEndian.PutUint64(data[24:32], m.lastValueTimestamp)

	// Write the key count
	binary.BigEndian.PutUint32(data[32:36], m.keyCount)

	// Write the sealed flag
	if m.sealed {
		data[36] = 1
	} else {
		data[36] = 0
	}

	return data
}

func (m *metadataFile) deserializeV0Legacy(data []byte) error {
	// TODO (cody.littley): delete this after all data is migrated
	if len(data) != V0MetadataSize {
		return fmt.Errorf("metadata file is not the correct size, expected %d, got %d",
			V0MetadataSize, len(data))
	}

	m.shardingFactor = binary.BigEndian.Uint32(data[4:8])
	m.legacySalt = binary.BigEndian.Uint32(data[8:12])
	m.lastValueTimestamp = binary.BigEndian.Uint64(data[12:20])
	m.sealed = data[20] == 1
	return nil
}

func (m *metadataFile) deserializeV1Legacy(data []byte) error {
	// TODO (cody.littley): delete this after all data is migrated
	if len(data) != V1MetadataSize {
		return fmt.Errorf("metadata file is not the correct size, expected %d, got %d",
			V1MetadataSize, len(data))
	}

	m.shardingFactor = binary.BigEndian.Uint32(data[4:8])
	m.salt = [16]byte(data[8:24])
	m.lastValueTimestamp = binary.BigEndian.Uint64(data[24:32])
	m.sealed = data[32] == 1
	return nil
}

// deserialize deserializes the metadata file from a byte array.
func (m *metadataFile) deserialize(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("metadata file is not the correct size, expected at least 4 bytes, got %d", len(data))
	}

	m.segmentVersion = SegmentVersion(binary.BigEndian.Uint32(data[0:4]))
	if m.segmentVersion > LatestSegmentVersion {
		return fmt.Errorf("unsupported serialization version: %d", m.segmentVersion)
	}

	if m.segmentVersion == OldHashFunctionSegmentVersion {
		return m.deserializeV0Legacy(data)
	} else if m.segmentVersion == SipHashSegmentVersion {
		return m.deserializeV1Legacy(data)
	}

	if len(data) != V2MetadataSize {
		return fmt.Errorf("metadata file is not the correct size, expected %d, got %d",
			V2MetadataSize, len(data))
	}

	m.shardingFactor = binary.BigEndian.Uint32(data[4:8])
	m.salt = [16]byte(data[8:24])
	m.lastValueTimestamp = binary.BigEndian.Uint64(data[24:32])
	m.keyCount = binary.BigEndian.Uint32(data[32:36])
	m.sealed = data[36] == 1

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
