package segment

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// ValuesFileExtension is the file extension for the values file. This file contains the values for the data
// segment. Value files are written in the form "X-Y.values", where X is the segment index and Y is the shard number.
const ValuesFileExtension = ".values"

// valueFile represents a file that stores values.
type valueFile struct {
	// The logger for the value file.
	logger logging.Logger

	// The segment index.
	index uint32

	// The shard number of this value file.
	shard uint32

	// Path data for the segment file.
	segmentPath *SegmentPath

	// The file wrapped by the writer. If the file is sealed, this value is nil.
	file *os.File

	// The writer for the file. If the file is sealed, this value is nil.
	writer *bufio.Writer

	// The current size of the file in bytes. Includes both flushed and unflushed data.
	size uint64

	// The current size of the file, only including flushed data. Protects against reads of partially written values.
	flushedSize atomic.Uint64

	// Whether fsync mode is enabled. If fsync mode is enabled, then each flush operation will invoke the OS fsync
	// operation before returning. An fsync operation is required to ensure that data is not sitting in OS level
	// in-memory buffers (otherwise, an OS crash may lead to data loss). This option is provided for testing,
	// as many test scenarios do lots of tiny writes and flushes, and this workload is MUCH slower with fsync
	// mode enabled. In production, fsync mode should always be enabled.
	fsync bool
}

// createValueFile creates a new value file.
func createValueFile(
	logger logging.Logger,
	index uint32,
	shard uint32,
	segmentPath *SegmentPath,
	fsync bool,
) (*valueFile, error) {

	values := &valueFile{
		logger:      logger,
		index:       index,
		shard:       shard,
		segmentPath: segmentPath,
		fsync:       fsync,
	}

	filePath := values.path()
	exists, _, err := util.ErrIfNotWritableFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("file %s has incorrect permissions: %v", filePath, err)
	}

	if exists {
		return nil, fmt.Errorf("value file %s already exists", filePath)
	}

	// Open the file for writing.
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open value file %s: %v", filePath, err)
	}

	values.file = file
	values.writer = bufio.NewWriter(file)

	return values, nil
}

// loadValueFile loads a value file from disk. It looks for the file in the given parent directories until it finds
// the file. If the file is not found, it returns an error.
func loadValueFile(
	logger logging.Logger,
	index uint32,
	shard uint32,
	segmentPaths []*SegmentPath) (*valueFile, error) {

	valuesFileName := fmt.Sprintf("%d-%d%s", index, shard, ValuesFileExtension)
	valuesPath, err := lookForFile(segmentPaths, valuesFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to find value file: %v", err)
	}
	if valuesPath == nil {
		return nil, fmt.Errorf("value file %s not found", valuesFileName)
	}

	values := &valueFile{
		logger:      logger,
		index:       index,
		shard:       shard,
		segmentPath: valuesPath,
		fsync:       false,
	}

	filePath := values.path()
	exists, size, err := util.ErrIfNotWritableFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("file %s has incorrect permissions: %v", filePath, err)
	}

	if !exists {
		return nil, fmt.Errorf("value file %s does not exist", filePath)
	}

	values.size = uint64(size)
	values.flushedSize.Store(values.size)

	return values, nil
}

// getValueFileIndex returns the index of the value file from the file name. Value file names have the form
// "X-Y.values", where X is the segment index and Y is the shard number.
func getValueFileIndex(fileName string) (uint32, error) {
	baseName := path.Base(fileName)
	strippedName := baseName[:len(baseName)-len(ValuesFileExtension)]

	parts := strings.Split(strippedName, "-")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid value file name %s", fileName)
	}
	indexString := parts[0]

	index, err := strconv.Atoi(indexString)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index from file name %s: %v", fileName, err)
	}

	return uint32(index), nil
}

// getValueFileShard returns the shard number of the value file from the file name. Value file names have the form
// "X-Y.values", where X is the segment index and Y is the shard number.
func getValueFileShard(fileName string) (uint32, error) {
	baseName := path.Base(fileName)
	strippedName := baseName[:len(baseName)-len(ValuesFileExtension)]

	parts := strings.Split(strippedName, "-")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid value file name %s", fileName)
	}
	shardString := parts[1]

	shard, err := strconv.Atoi(shardString)
	if err != nil {
		return 0, fmt.Errorf("failed to parse shard from file name %s: %v", fileName, err)
	}

	return uint32(shard), nil
}

// Size returns the size of the value file in bytes.
func (v *valueFile) Size() uint64 {
	return v.size
}

// name returns the name of the value file.
func (v *valueFile) name() string {
	return fmt.Sprintf("%d-%d%s", v.index, v.shard, ValuesFileExtension)
}

// path returns the path to the value file.
func (v *valueFile) path() string {
	return path.Join(v.segmentPath.SegmentDirectory(), v.name())
}

// read reads a value from the value file.
func (v *valueFile) read(firstByteIndex uint32) ([]byte, error) {
	flushedSize := v.flushedSize.Load()
	if uint64(firstByteIndex) >= flushedSize {
		return nil, fmt.Errorf("index %d is out of bounds (current flushed size is %d)",
			firstByteIndex, flushedSize)
	}

	file, err := os.OpenFile(v.path(), os.O_RDONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open value file: %v", err)
	}
	defer func() {
		err = file.Close()
		if err != nil {
			v.logger.Errorf("failed to close value file: %v", err)
		}
	}()

	_, err = file.Seek(int64(firstByteIndex), 0)
	reader := bufio.NewReader(file)

	// Read the length of the value.
	var length uint32
	err = binary.Read(reader, binary.BigEndian, &length)
	if err != nil {
		return nil, fmt.Errorf("failed to read value length from value file: %v", err)
	}

	// Read the value itself.
	value := make([]byte, length)
	bytesRead, err := io.ReadFull(reader, value)
	if err != nil {
		return nil, fmt.Errorf("failed to read value from value file: %v", err)
	}

	if uint32(bytesRead) != length {
		return nil, fmt.Errorf("failed to read value from value file: read %d bytes, expected %d", bytesRead, length)
	}

	return value, nil
}

// write writes a value to the value file, returning the index of the first byte written.
func (v *valueFile) write(value []byte) (uint32, error) {
	if v.writer == nil {
		return 0, fmt.Errorf("value file is sealed")
	}

	if v.size > math.MaxUint32 {
		// No matter what, we can't start a new value if its first byte would be beyond position 2^32.
		// This is because we only have 32 bits in an address to store the position of a value's first byte.
		return 0, fmt.Errorf("value file already contains %d bytes, cannot add a new value", v.size)
	}

	firstByteIndex := uint32(v.size)

	// First, write the length of the value.
	err := binary.Write(v.writer, binary.BigEndian, uint32(len(value)))
	if err != nil {
		return 0, fmt.Errorf("failed to write value length to value file: %v", err)
	}

	// Then, write the value itself.
	_, err = v.writer.Write(value)
	if err != nil {
		return 0, fmt.Errorf("failed to write value to value file: %v", err)
	}

	v.size += uint64(len(value) + 4)

	return firstByteIndex, nil
}

// flush writes all unflushed data to disk.
func (v *valueFile) flush() error {
	if v.writer == nil {
		return fmt.Errorf("value file is sealed")
	}

	err := v.writer.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush value file: %v", err)
	}

	if v.fsync {
		err = v.file.Sync()
		if err != nil {
			return fmt.Errorf("failed to sync value file: %v", err)
		}
	}

	// It is now safe to read the flushed bytes directly from the file.
	v.flushedSize.Store(v.size)

	return nil
}

// seal seals the value file.
func (v *valueFile) seal() error {
	if v.writer == nil {
		return fmt.Errorf("value file is already sealed")
	}

	err := v.flush()
	if err != nil {
		return fmt.Errorf("failed to flush value file: %v", err)
	}

	err = v.file.Close()
	if err != nil {
		return fmt.Errorf("failed to close value file: %v", err)
	}

	v.writer = nil
	v.file = nil
	return nil
}

// snapshot creates a hard link to the file in the snapshot directory, and a soft link to the hard linked file in the
// soft link directory. Requires that the file is sealed and that snapshotting is enabled.
func (v *valueFile) snapshot() error {
	if v.writer != nil {
		return fmt.Errorf("file %s is not sealed, cannot take Snapshot", v.path())
	}

	err := v.segmentPath.Snapshot(v.name())
	if err != nil {
		return fmt.Errorf("failed to create Snapshot: %v", err)
	}

	return nil
}

// delete deletes the value file.
func (v *valueFile) delete() error {
	if v.writer != nil {
		return fmt.Errorf("value file is not sealed")
	}

	// As an extra safety check, make it so that all future reads fail before they do I/O.
	v.flushedSize.Store(0)

	err := util.DeepDelete(v.path())
	if err != nil {
		return fmt.Errorf("failed to delete value file %s: %v", v.path(), err)
	}

	return nil
}
