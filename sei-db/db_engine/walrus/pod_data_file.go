package walrus

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The first bytes of every pod data file, identifying the format.
var podDataMagic = []byte("WALRSPOD")

// The version of the pod data file layout, validated on read so a file written by an incompatible build is
// refused rather than misparsed.
const podDataFormatVersion = byte(1)

// The size of a pod data file's header: 8 byte magic, 1 byte version, 8 byte first block, 8 byte last block.
const podDataHeaderSize = 25

// Marks an entry as a deletion rather than a write. A walk that reaches one answers ReadAbsent instead of
// continuing to older pods.
const entryFlagDeleted = byte(1 << 0)

// The largest entry a pod may hold. A length prefix above this is treated as corruption rather than trusted
// into an allocation.
const maxEntrySize = 64 * 1024 * 1024

var _ PodReader = (*podReader)(nil)

// podReader reads single entries out of a pod data file by byte offset.
//
// It holds the file's path and nothing from the file itself, opening it for the duration of a read. Reads
// only happen after a bloom filter and an index have both named this pod, so they are rare relative to the
// probes that precede them.
type podReader struct {
	path string
	info *PodInfo
	size int64
}

// ReadEntry returns the value and deletion flag of the entry at the given offset into the data section.
func (r *podReader) ReadEntry(offset uint32) (value []byte, deleted bool, err error) {
	file, err := os.Open(r.path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, false, fmt.Errorf("failed to open pod data file %s: %w", r.path, err)
	}
	defer func() { _ = file.Close() }()

	// An entry is length prefixed, so how far it extends is not known until part of it has been read. Pull a
	// window big enough for the prefixes and most entries, and widen it only when the entry says it is longer.
	start := int64(podDataHeaderSize) + int64(offset)
	window, err := readWindow(file, start, 512)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read entry at offset %d of %s: %w", offset, r.path, err)
	}

	entry, needed, err := decodeEntry(window)
	if err != nil {
		return nil, false, fmt.Errorf("failed to decode entry at offset %d of %s: %w", offset, r.path, err)
	}
	if needed > 0 {
		if needed > maxEntrySize {
			return nil, false, fmt.Errorf("entry at offset %d of %s claims %d bytes",
				offset, r.path, needed)
		}
		window, err = readWindow(file, start, needed)
		if err != nil {
			return nil, false, fmt.Errorf("failed to read %d byte entry at offset %d of %s: %w",
				needed, offset, r.path, err)
		}
		entry, needed, err = decodeEntry(window)
		if err != nil {
			return nil, false, fmt.Errorf("failed to decode entry at offset %d of %s: %w",
				offset, r.path, err)
		}
		if needed > 0 {
			return nil, false, fmt.Errorf("entry at offset %d of %s runs past the end of the file",
				offset, r.path)
		}
	}

	value = make([]byte, len(entry.value))
	copy(value, entry.value)
	return value, entry.deleted, nil
}

// Path returns the file the pod's data lives in.
func (r *podReader) Path() string {
	return r.path
}

// Size returns the bytes the file occupies.
func (r *podReader) Size() int64 {
	return r.size
}

// Delete removes the file.
func (r *podReader) Delete() error {
	if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete pod data file %s: %w", r.path, err)
	}
	return nil
}

// openPodReader returns a reader over an existing pod data file.
func openPodReader(path string) (*podReader, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod data file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pod data file %s: %w", path, err)
	}

	header := make([]byte, podDataHeaderSize)
	if _, err := file.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("failed to read pod data header from %s: %w", path, err)
	}
	info, err := parsePodDataHeader(header, path)
	if err != nil {
		return nil, err
	}

	return &podReader{path: path, info: info, size: stat.Size()}, nil
}

// parsePodDataHeader validates a pod data file's header and reports the blocks it covers.
func parsePodDataHeader(header []byte, path string) (*PodInfo, error) {
	if len(header) != podDataHeaderSize {
		return nil, fmt.Errorf("pod data file %s has a truncated header", path)
	}
	if string(header[:len(podDataMagic)]) != string(podDataMagic) {
		return nil, fmt.Errorf("pod data file %s is not a pod: bad magic", path)
	}
	version := header[len(podDataMagic)]
	if version != podDataFormatVersion {
		return nil, fmt.Errorf("pod data file %s has format version %d, expected %d",
			path, version, podDataFormatVersion)
	}
	firstBlock := binary.BigEndian.Uint64(header[9:17])
	lastBlock := binary.BigEndian.Uint64(header[17:25])
	if lastBlock < firstBlock {
		return nil, fmt.Errorf("pod data file %s claims blocks [%d, %d]", path, firstBlock, lastBlock)
	}
	return &PodInfo{FirstBlock: firstBlock, LastBlock: lastBlock}, nil
}

// readWindow reads up to length bytes starting at offset, tolerating a short read at the end of the file.
func readWindow(file *os.File, offset int64, length int) ([]byte, error) {
	window := make([]byte, length)
	read, err := file.ReadAt(window, offset)
	if read == 0 && err != nil {
		return nil, fmt.Errorf("failed to read %d bytes at %d: %w", length, offset, err)
	}
	return window[:read], nil
}

// podEntryRef locates one entry of a pod within its data section. It is what the index is built from.
type podEntryRef struct {
	// The first 8 bytes of the key, which level one of the index is ordered by.
	prefix uint64

	// The key, aliasing the changeset it came from rather than a copy of it.
	key []byte

	// The block that wrote the entry, relative to the pod's first block.
	blockDelta uint32

	// The entry's byte offset into the data section.
	offset uint32
}

// writePodData writes a pod's data file and returns a reference to every entry it holds.
//
// blocks must be non-empty and in contiguous ascending order. The references come back in write order, which
// is what makes the last write of a key within one block the one that survives sorting.
func writePodData(path string, blocks []Block) (refs []podEntryRef, size int64, err error) {
	firstBlock := blocks[0].Number
	lastBlock := blocks[len(blocks)-1].Number

	file, err := os.Create(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create pod data file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriterSize(file, 1<<20)
	if err := writePodDataHeader(writer, firstBlock, lastBlock); err != nil {
		return nil, 0, err
	}

	refs = make([]podEntryRef, 0, estimateEntryCount(blocks))
	var cursor uint64
	record := make([]byte, 0, 1<<16)

	for _, block := range blocks {
		blockDelta := block.Number - firstBlock
		record, refs = encodeBlockRecord(record[:0], refs, block, blockDelta, cursor)
		if _, err := writer.Write(record); err != nil {
			return nil, 0, fmt.Errorf("failed to write block %d to %s: %w", block.Number, path, err)
		}
		cursor += uint64(len(record))
		if cursor > maxPodDataSize {
			return nil, 0, fmt.Errorf(
				"pod data section reached %d bytes, past the %d byte addressable limit",
				cursor, uint64(maxPodDataSize))
		}
	}

	if err := writer.Flush(); err != nil {
		return nil, 0, fmt.Errorf("failed to flush pod data file %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return nil, 0, fmt.Errorf("failed to sync pod data file %s: %w", path, err)
	}

	//nolint:gosec // G115 - the cursor is bounded by maxPodDataSize above
	return refs, int64(podDataHeaderSize) + int64(cursor), nil
}

// writePodDataHeader writes the magic, format version, and block range that open a pod data file.
func writePodDataHeader(writer *bufio.Writer, firstBlock uint64, lastBlock uint64) error {
	header := make([]byte, 0, podDataHeaderSize)
	header = append(header, podDataMagic...)
	header = append(header, podDataFormatVersion)
	header = binary.BigEndian.AppendUint64(header, firstBlock)
	header = binary.BigEndian.AppendUint64(header, lastBlock)
	if _, err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write pod data header: %w", err)
	}
	return nil
}

// encodeBlockRecord appends one block's record to record and a reference for each of its entries to refs.
//
// dataOffset is where the record starts within the data section, which is what makes the offsets recorded in
// refs absolute rather than record-relative.
func encodeBlockRecord(
	record []byte,
	refs []podEntryRef,
	block Block,
	blockDelta uint64,
	dataOffset uint64,
) ([]byte, []podEntryRef) {
	record = binary.AppendUvarint(record, blockDelta)
	entryCount := uint64(countPairs(block)) //nolint:gosec // G115 - bounded by the pod size cap
	record = binary.AppendUvarint(record, entryCount)

	for _, changeSet := range block.ChangeSets {
		for _, pair := range changeSet.Changeset.Pairs {
			//nolint:gosec // G115 - bounded by maxPodDataSize, checked by the caller after each block
			refs = append(refs, podEntryRef{
				prefix:     keyPrefix(pair.Key),
				key:        pair.Key,
				blockDelta: uint32(blockDelta),
				offset:     uint32(dataOffset + uint64(len(record))),
			})
			record = encodeEntry(record, pair)
		}
	}

	checksum := crc32.ChecksumIEEE(record)
	return binary.BigEndian.AppendUint32(record, checksum), refs
}

// encodeEntry appends one key/value change to a block record.
func encodeEntry(record []byte, pair *proto.KVPair) []byte {
	record = binary.AppendUvarint(record, uint64(len(pair.Key)))
	record = append(record, pair.Key...)
	var flags byte
	if pair.Delete {
		flags |= entryFlagDeleted
	}
	record = append(record, flags)
	record = binary.AppendUvarint(record, uint64(len(pair.Value)))
	return append(record, pair.Value...)
}

// countPairs reports how many key changes a block holds across all of its changesets.
func countPairs(block Block) int {
	count := 0
	for _, changeSet := range block.ChangeSets {
		count += len(changeSet.Changeset.Pairs)
	}
	return count
}

// estimateEntryCount reports how many entries the blocks hold, so the reference slice is allocated once.
func estimateEntryCount(blocks []Block) int {
	count := 0
	for _, block := range blocks {
		count += countPairs(block)
	}
	return count
}

// decodedEntry is one entry read out of a pod's data section.
type decodedEntry struct {
	key     []byte
	value   []byte
	deleted bool
}

// decodeEntry decodes the entry at the front of buffer.
//
// When the buffer is too short to hold the whole entry, needed reports how many bytes to read instead and
// entry is meaningless. needed is 0 when the entry was decoded.
func decodeEntry(buffer []byte) (entry decodedEntry, needed int, err error) {
	keyLength, keyLengthSize := binary.Uvarint(buffer)
	if keyLengthSize <= 0 {
		return decodedEntry{}, len(buffer)*2 + 16, nil
	}
	cursor := keyLengthSize
	if keyLength > maxEntrySize {
		return decodedEntry{}, 0, fmt.Errorf("entry claims a %d byte key", keyLength)
	}
	if uint64(len(buffer)-cursor) < keyLength { //nolint:gosec // G115 - the cursor never exceeds the buffer length
		return decodedEntry{}, cursor + int(keyLength) + binary.MaxVarintLen64 + 1, nil
	}
	key := buffer[cursor : cursor+int(keyLength)]
	cursor += int(keyLength)

	if cursor >= len(buffer) {
		return decodedEntry{}, len(buffer)*2 + 16, nil
	}
	flags := buffer[cursor]
	cursor++

	valueLength, valueLengthSize := binary.Uvarint(buffer[cursor:])
	if valueLengthSize <= 0 {
		return decodedEntry{}, len(buffer)*2 + 16, nil
	}
	cursor += valueLengthSize
	if valueLength > maxEntrySize {
		return decodedEntry{}, 0, fmt.Errorf("entry claims a %d byte value", valueLength)
	}
	remaining := uint64(len(buffer) - cursor) //nolint:gosec // G115 - the cursor stays within the buffer
	if remaining < valueLength {
		return decodedEntry{}, cursor + int(valueLength), nil
	}

	return decodedEntry{
		key:     key,
		value:   buffer[cursor : cursor+int(valueLength)],
		deleted: flags&entryFlagDeleted != 0,
	}, 0, nil
}
