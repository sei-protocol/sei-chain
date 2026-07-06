package segment

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
)

// valueReaderBufferSize is the size of the buffer used for sequential value-file reads.
const valueReaderBufferSize = 64 * unit.KB

// SegmentReader provides buffered, mostly-sequential reads of a sealed segment's values. It is intended
// for linear scans (e.g. a forward iterator): it holds one open, buffered reader per shard value file
// and advances each sequentially, falling back to a seek only when a requested value does not begin at
// the reader's current position.
//
// A SegmentReader is owned by its caller and is NOT safe for concurrent use. The Segment itself carries
// no per-reader state, so any number of SegmentReaders may be open against the same segment at once.
// Close must be called to release the underlying file handles.
type SegmentReader struct {
	// segment is the segment being read.
	segment *Segment

	// readers holds one buffered reader per shard, indexed by shard ID. Each is opened lazily the first
	// time a value on that shard is read.
	readers []*valueFileReader
}

// NewReader creates a SegmentReader for this segment. The segment must be sealed.
func (s *Segment) NewReader() *SegmentReader {
	return &SegmentReader{
		segment: s,
		readers: make([]*valueFileReader, len(s.shards)),
	}
}

// Read returns the value at the given address, reading sequentially from the relevant shard's value file
// when possible.
func (r *SegmentReader) Read(address types.Address) ([]byte, error) {
	shardID := address.ShardID()
	if int(shardID) >= len(r.readers) {
		return nil, fmt.Errorf("shard ID %d out of range for segment %d (sharding factor %d)",
			shardID, r.segment.index, len(r.readers))
	}

	if r.readers[shardID] == nil {
		reader, err := r.segment.shards[shardID].newReader()
		if err != nil {
			return nil, fmt.Errorf("failed to open value file reader for shard %d: %w", shardID, err)
		}
		r.readers[shardID] = reader
	}

	return r.readers[shardID].read(address.Offset(), address.ValueSize())
}

// Close releases all file handles held by the reader. It is safe to call more than once.
func (r *SegmentReader) Close() error {
	var firstErr error
	for i, reader := range r.readers {
		if reader == nil {
			continue
		}
		if err := reader.close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close value file reader: %w", err)
		}
		r.readers[i] = nil
	}
	return firstErr
}

// valueFileReader is a buffered reader over a single shard's value file. It tracks its current read
// position so that consecutive in-order reads are served sequentially from the buffer without seeking.
// It is owned by a SegmentReader and is NOT safe for concurrent use.
type valueFileReader struct {
	// path is the value file path, retained for error messages.
	path string

	// file is the open value file handle.
	file *os.File

	// reader is the buffered reader wrapping file.
	reader *bufio.Reader

	// position is the byte offset within the file that the next sequential read will start from.
	position uint64

	// flushedSize is the number of bytes that are safe to read. Captured at open time; a sealed value
	// file is immutable, so this does not change.
	flushedSize uint64
}

// newReader opens a buffered reader over the value file. The value file should be sealed.
func (v *valueFile) newReader() (*valueFileReader, error) {
	file, err := os.OpenFile(v.path(), os.O_RDONLY, 0600) //nolint:gosec // path validated by segment manager
	if err != nil {
		return nil, fmt.Errorf("failed to open value file %s: %w", v.path(), err)
	}
	return &valueFileReader{
		path:        v.path(),
		file:        file,
		reader:      bufio.NewReaderSize(file, valueReaderBufferSize),
		position:    0,
		flushedSize: v.flushedSize.Load(),
	}, nil
}

// read returns the length-byte value starting at firstByteIndex. When firstByteIndex equals the reader's
// current position the value is read sequentially from the buffer; otherwise the reader seeks to
// firstByteIndex and discards its buffer first.
func (r *valueFileReader) read(firstByteIndex uint32, length uint32) ([]byte, error) {
	end := uint64(firstByteIndex) + uint64(length)
	if end > r.flushedSize {
		return nil, fmt.Errorf("range [%d, %d) is out of bounds (flushed size is %d)",
			firstByteIndex, end, r.flushedSize)
	}

	if uint64(firstByteIndex) != r.position {
		// The requested range does not begin where the buffered reader is positioned (a skip). Seek the
		// underlying file and discard the buffer before reading.
		_, err := r.file.Seek(int64(firstByteIndex), io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to seek value file %s: %w", r.path, err)
		}
		r.reader.Reset(r.file)
		r.position = uint64(firstByteIndex)
	}

	value := make([]byte, length)
	_, err := io.ReadFull(r.reader, value)
	if err != nil {
		return nil, fmt.Errorf("failed to read value from value file %s: %w", r.path, err)
	}
	r.position += uint64(length)

	return value, nil
}

// close closes the underlying file handle.
func (r *valueFileReader) close() error {
	return r.file.Close()
}
