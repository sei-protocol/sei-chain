package disktable

import (
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"sync/atomic"
	"time"

	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

const tableMetadataSerializationVersion = 0
const TableMetadataFileName = "table.metadata"
const tableMetadataSize = 16

// tableMetadata contains table data that is preserved across restarts.
type tableMetadata struct {
	logger logging.Logger

	tableDirectory string

	// the table's TTL, accessed/modified by concurrent goroutines
	ttl atomic.Pointer[time.Duration]

	// the table's sharding factor, accessed/modified by concurrent goroutines
	shardingFactor atomic.Uint32

	// If true, metadata writes will be atomic. Should be set to true in production, but can be set to false
	// to speed up unit tests.
	fsync bool
}

// newTableMetadata creates a new table metadata object.
func newTableMetadata(
	logger logging.Logger,
	tableDirectory string,
	ttl time.Duration,
	shardingFactor uint32,
	fsync bool) (*tableMetadata, error) {

	metadata := &tableMetadata{
		logger:         logger,
		tableDirectory: tableDirectory,
		fsync:          fsync,
	}
	metadata.ttl.Store(&ttl)
	metadata.shardingFactor.Store(shardingFactor)

	err := metadata.write()
	if err != nil {
		return nil, fmt.Errorf("failed to write table metadata: %v", err)
	}

	return metadata, nil
}

// loadTableMetadata loads the table metadata from disk.
func loadTableMetadata(logger logging.Logger, tableDirectory string) (*tableMetadata, error) {
	mPath := metadataPath(tableDirectory)

	if err := util.ErrIfNotExists(mPath); err != nil {
		return nil, fmt.Errorf("table metadata file does not exist: %s", mPath)
	}

	data, err := os.ReadFile(mPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read table metadata file %s: %v", mPath, err)
	}

	metadata, err := deserialize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize table metadata: %v", err)
	}
	metadata.logger = logger
	metadata.tableDirectory = tableDirectory

	return metadata, nil
}

// Size returns the size of the table metadata file in bytes.
func (t *tableMetadata) Size() uint64 {
	return tableMetadataSize
}

// GetTTL returns the time-to-live for the table.
func (t *tableMetadata) GetTTL() time.Duration {
	return *t.ttl.Load()
}

// SetTTL sets the time-to-live for the table.
func (t *tableMetadata) SetTTL(ttl time.Duration) error {
	t.ttl.Store(&ttl)
	err := t.write()
	if err != nil {
		return fmt.Errorf("failed to update table metadata: %v", err)
	}
	return nil
}

// GetShardingFactor returns the sharding factor for the table.
func (t *tableMetadata) GetShardingFactor() uint32 {
	return t.shardingFactor.Load()
}

// SetShardingFactor sets the sharding factor for the table.
func (t *tableMetadata) SetShardingFactor(shardingFactor uint32) error {
	t.shardingFactor.Store(shardingFactor)
	err := t.write()
	if err != nil {
		return fmt.Errorf("failed to update table metadata: %v", err)
	}
	return nil
}

// Store atomically stores the table metadata to disk.
func (t *tableMetadata) write() error {
	err := util.AtomicWrite(metadataPath(t.tableDirectory), t.serialize(), t.fsync)
	if err != nil {
		return fmt.Errorf("failed to write table metadata file: %v", err)
	}

	return nil
}

// serialize serializes the table metadata to a byte slice.
func (t *tableMetadata) serialize() []byte {
	// 4 bytes for version
	// 8 bytes for TTL
	// 4 bytes for sharding factor
	data := make([]byte, tableMetadataSize)

	// Write the version
	binary.BigEndian.PutUint32(data[0:4], tableMetadataSerializationVersion)

	// Write the TTL
	ttlNanoseconds := t.GetTTL().Nanoseconds()
	binary.BigEndian.PutUint64(data[4:12], uint64(ttlNanoseconds))

	// Write the sharding factor
	binary.BigEndian.PutUint32(data[12:16], t.GetShardingFactor())

	return data
}

// deserialize deserializes the table metadata from a byte slice.
func deserialize(data []byte) (*tableMetadata, error) {
	// 4 bytes for version
	// 8 bytes for TTL
	// 4 bytes for sharding factor
	if len(data) != tableMetadataSize {
		return nil, fmt.Errorf("metadata file is not the correct size, expected 16 bytes, got %d", len(data))
	}

	serializationVersion := binary.BigEndian.Uint32(data[0:4])
	if serializationVersion != tableMetadataSerializationVersion {
		return nil, fmt.Errorf("unsupported serialization version: %d", serializationVersion)
	}

	ttl := time.Duration(binary.BigEndian.Uint64(data[4:12]))
	shardingFactor := binary.BigEndian.Uint32(data[12:16])

	metadata := &tableMetadata{}
	metadata.ttl.Store(&ttl)
	metadata.shardingFactor.Store(shardingFactor)

	return metadata, nil
}

// delete deletes the table metadata from disk.
func (t *tableMetadata) delete() error {
	metadataPath := path.Join(t.tableDirectory, TableMetadataFileName)
	err := os.Remove(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to delete table metadata file %s: %v", metadataPath, err)
	}
	return nil
}

// path returns the path to the table metadata file.
func metadataPath(tableDirectory string) string {
	return path.Join(tableDirectory, TableMetadataFileName)
}
