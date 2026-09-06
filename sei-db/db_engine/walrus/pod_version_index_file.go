package walrus

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

// The first bytes of every pod version index file, identifying the format.
var podVersionIndexMagic = []byte("WALRSVER")

// The version of the pod version index layout, validated on read so an index written by an incompatible
// build is refused rather than misparsed.
const podVersionIndexFormatVersion = byte(1)

// The size of a pod version index header: 8 byte magic, 1 byte version, 8 byte key count, 8 byte offset of
// the key records.
const podVersionIndexHeaderSize = 25

// The size of one pointer: the offset of a key's record within the records.
//
// It is 64 bit because the records can outgrow the data section they describe. A version entry costs eight
// bytes however few bytes the write cost in the pod, so a pod of mostly distinct keys with short values
// fills the records past four gigabytes while its data section still fits.
const indexPointerSize = 8

// The size of one version entry in a key record: block delta and entry offset, both uint32.
const indexVersionSize = 8

// The size of the key length that opens a key record.
const indexRecordKeyLengthSize = 2

// The size of the version count that follows a key record's key.
const indexRecordVersionCountSize = 4

// How much of a key record is read before its length is known. It covers the whole of all but the records of
// keys written many times, which are read again once their version count says how far they extend.
const indexRecordWindowSize = 512

// podVersionIndex is the dereferenced half of a pod's index: where each key's versions live, and the key
// itself.
//
// It opens with a pointer per distinct key, positionally matching the hash index, followed by the key
// records those pointers address. A record holds the whole key, so a search confirms the key it landed on
// without reading the pod, and two keys that share a hash are told apart here.
//
// It holds the file's path and nothing from the file itself, opening it for the duration of a read. Unlike
// the hash index it is touched once per search rather than at every probe, and it is an order of magnitude
// larger, so mapping it would spend the address space and page cache the hash index needs on pages used
// once.
type podVersionIndex struct {
	path         string
	size         int64
	keyCount     uint64
	recordsStart int64
}

// indexRecord is one key record: the key itself and every version of it the pod holds.
type indexRecord struct {
	key      []byte
	versions []byte
}

// readRecord returns the key record the given entry of the hash index points at.
func (v *podVersionIndex) readRecord(slot uint64) (*indexRecord, error) {
	if slot >= v.keyCount {
		return nil, fmt.Errorf("pod version index %s was asked for key %d of %d", v.path, slot, v.keyCount)
	}

	file, err := os.Open(v.path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod version index %s: %w", v.path, err)
	}
	defer func() { _ = file.Close() }()

	//nolint:gosec // G115 - the slot is below the key count, which the header size check bounds
	pointerAt := int64(podVersionIndexHeaderSize) + int64(slot*indexPointerSize)
	var pointer [indexPointerSize]byte
	if _, err := file.ReadAt(pointer[:], pointerAt); err != nil {
		return nil, fmt.Errorf("failed to read pointer %d of %s: %w", slot, v.path, err)
	}
	recordOffset := binary.BigEndian.Uint64(pointer[:])

	//nolint:gosec // G115 - the offset is range checked against the file size below
	start := v.recordsStart + int64(recordOffset)
	if start < v.recordsStart || start >= v.size {
		return nil, fmt.Errorf("pod version index %s puts key %d at %d of %d bytes",
			v.path, slot, recordOffset, v.size)
	}

	window, err := readWindow(file, start, indexRecordWindowSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read the record for key %d of %s: %w", slot, v.path, err)
	}
	record, needed, err := decodeIndexRecord(window)
	if err != nil {
		return nil, fmt.Errorf("failed to decode the record for key %d of %s: %w", slot, v.path, err)
	}
	if needed == 0 {
		return record, nil
	}

	// The version count said the record runs past the window. It cannot run past the file.
	if int64(needed) > v.size-start {
		return nil, fmt.Errorf("the record for key %d of %s claims %d bytes", slot, v.path, needed)
	}
	window, err = readWindow(file, start, needed)
	if err != nil {
		return nil, fmt.Errorf("failed to read the %d byte record for key %d of %s: %w",
			needed, slot, v.path, err)
	}
	record, needed, err = decodeIndexRecord(window)
	if err != nil {
		return nil, fmt.Errorf("failed to decode the record for key %d of %s: %w", slot, v.path, err)
	}
	if needed > 0 {
		return nil, fmt.Errorf("the record for key %d of %s runs past the end of the file", slot, v.path)
	}
	return record, nil
}

// Path returns the file the version index lives in.
func (v *podVersionIndex) Path() string {
	return v.path
}

// Size returns the bytes the file occupies.
func (v *podVersionIndex) Size() int64 {
	return v.size
}

// Delete removes the file.
func (v *podVersionIndex) Delete() error {
	if err := os.Remove(v.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete pod version index %s: %w", v.path, err)
	}
	return nil
}

// newestInRange returns the entry offset of the newest version whose block delta lies in
// (lowDelta, highDelta].
func (r *indexRecord) newestInRange(lowDelta int64, highDelta int64) (offset uint32, delta uint32, found bool) {
	count := len(r.versions) / indexVersionSize

	// Versions ascend by block, so the newest in range is the one just before the first that exceeds it.
	above := sort.Search(count, func(candidate int) bool {
		return int64(binary.BigEndian.Uint32(r.versions[candidate*indexVersionSize:])) > highDelta
	})
	if above == 0 {
		return 0, 0, false
	}
	entry := r.versions[(above-1)*indexVersionSize:]
	delta = binary.BigEndian.Uint32(entry[0:4])
	if int64(delta) <= lowDelta {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(entry[4:8]), delta, true
}

// decodeIndexRecord reads one key record from the front of window.
//
// needed is non-zero when the window stopped short of the whole record, and reports how many bytes reading
// it again would take. Every length the record supplies is checked against what the window actually holds
// before it is used to slice, since a corrupt file could otherwise describe a record longer than the file.
func decodeIndexRecord(window []byte) (record *indexRecord, needed int, err error) {
	if len(window) < indexRecordKeyLengthSize {
		return nil, 0, fmt.Errorf("a key record is truncated at %d bytes", len(window))
	}
	keyLength := int(binary.BigEndian.Uint16(window))
	cursor := indexRecordKeyLengthSize

	if cursor+keyLength+indexRecordVersionCountSize > len(window) {
		return nil, cursor + keyLength + indexRecordVersionCountSize, nil
	}
	key := window[cursor : cursor+keyLength]
	cursor += keyLength

	versionCount := int(binary.BigEndian.Uint32(window[cursor:]))
	cursor += indexRecordVersionCountSize
	if versionCount < 0 {
		return nil, 0, fmt.Errorf("a key record claims %d versions", versionCount)
	}
	versionBytes := versionCount * indexVersionSize
	if cursor+versionBytes > len(window) {
		return nil, cursor + versionBytes, nil
	}

	return &indexRecord{key: key, versions: window[cursor : cursor+versionBytes]}, 0, nil
}

// openPodVersionIndex opens an existing pod version index file for reading.
func openPodVersionIndex(path string) (*podVersionIndex, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod version index %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pod version index %s: %w", path, err)
	}
	size := stat.Size()
	if size < podVersionIndexHeaderSize {
		return nil, fmt.Errorf("pod version index %s is truncated at %d bytes", path, size)
	}

	header := make([]byte, podVersionIndexHeaderSize)
	if _, err := file.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("failed to read the header of pod version index %s: %w", path, err)
	}
	if string(header[:len(podVersionIndexMagic)]) != string(podVersionIndexMagic) {
		return nil, fmt.Errorf("pod version index %s is not a version index: bad magic", path)
	}
	if version := header[len(podVersionIndexMagic)]; version != podVersionIndexFormatVersion {
		return nil, fmt.Errorf("pod version index %s has format version %d, expected %d",
			path, version, podVersionIndexFormatVersion)
	}

	keyCount := binary.BigEndian.Uint64(header[9:17])
	recordsStart := binary.BigEndian.Uint64(header[17:25])

	// The pointers have to hold exactly keyCount of them and the records have to start where they end, or
	// every offset a search derives from them is meaningless.
	expected := uint64(podVersionIndexHeaderSize) + keyCount*indexPointerSize
	if recordsStart != expected || recordsStart > uint64(size) { //nolint:gosec // G115 - a size is never negative
		return nil, fmt.Errorf("pod version index %s puts %d keys and its records at %d of %d bytes",
			path, keyCount, recordsStart, size)
	}

	return &podVersionIndex{
		path:         path,
		size:         size,
		keyCount:     keyCount,
		recordsStart: int64(recordsStart), //nolint:gosec // G115 - bounded by the file size above
	}, nil
}

// writePodVersionIndex writes a pod's version index from its pointers and key records.
func writePodVersionIndex(path string, pointers []byte, records []byte, keyCount uint64) (size int64, err error) {
	recordsStart := uint64(podVersionIndexHeaderSize) + uint64(len(pointers))

	header := make([]byte, 0, podVersionIndexHeaderSize)
	header = append(header, podVersionIndexMagic...)
	header = append(header, podVersionIndexFormatVersion)
	header = binary.BigEndian.AppendUint64(header, keyCount)
	header = binary.BigEndian.AppendUint64(header, recordsStart)

	if err := writeFileParts(path, header, pointers, records); err != nil {
		return 0, err
	}
	return int64(len(header)) + int64(len(pointers)) + int64(len(records)), nil
}
