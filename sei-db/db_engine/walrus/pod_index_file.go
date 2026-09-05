package walrus

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"

	"golang.org/x/sys/unix"
)

// The first bytes of every pod index file, identifying the format.
var podIndexMagic = []byte("WALRSIDX")

// The version of the pod index layout, validated on read so an index written by an incompatible build is
// refused rather than misparsed.
const podIndexFormatVersion = byte(1)

// The size of a pod index header: 8 byte magic, 1 byte version, 8 byte first block, 8 byte last block,
// 8 byte key count, 8 byte level two offset.
const podIndexHeaderSize = 41

// The size of one level one slot: an 8 byte key prefix and the 4 byte offset of its level two record.
const indexSlotSize = 12

// The size of one version entry in a level two record: block delta and entry offset, both uint32.
const indexVersionSize = 8

// The size of a level two record's fixed part: a 2 byte key length before the key, and a 4 byte version
// count after it.
const indexRecordKeyLengthSize = 2

// The size of the version count that follows a level two record's key.
const indexRecordVersionCountSize = 4

var _ PodIndex = (*podIndex)(nil)

// podIndex searches a pod index file.
//
// The file is memory mapped rather than read through. A search is a binary search over level one, which at a
// million keys to a pod is twenty probes of twelve bytes each: reading those through the file interface costs
// a syscall apiece to move what the kernel already paged in, and the last several probes land inside a single
// page anyway. Mapping makes the search plain slice arithmetic, and it costs no file descriptor because the
// mapping outlives the one it was created from.
//
// Mapping is not residency. The kernel pages in what a search touches and evicts under pressure, which is the
// behaviour wanted for an index far larger than memory.
//
// Offsets read out of the file are validated before they are used to slice. Against a mapping a bad offset
// would panic or read neighbouring bytes as a record, where a read through the file interface would simply
// have failed.
//
// The mapping is released by Delete, which is why the catalog will not delete a pod a query still references:
// unlinking a file underneath a reader is safe, but unmapping memory it is still reading is not.
type podIndex struct {
	path     string
	info     *PodInfo
	size     int64
	keyCount uint64

	// The whole file as mapped, which is what Munmap has to be given back.
	mapping []byte

	// Level one, a fixed width array of slots ordered by key.
	level1 []byte

	// Level two, one record per distinct key in the same order.
	level2 []byte
}

// FindNewest returns where the newest version of key written in (lowBlock, highBlock] lives.
func (i *podIndex) FindNewest(key []byte, lowBlock uint64, highBlock uint64) (
	offset uint32,
	blockNumber uint64,
	found bool,
	present bool,
	err error,
) {
	lowDelta, highDelta, overlaps := i.deltaRange(lowBlock, highBlock)
	if i.keyCount == 0 {
		return 0, 0, false, false, nil
	}

	target := keyPrefix(key)
	slot := i.searchLevelOne(target)

	// Slots sharing a key prefix form a contiguous run, since level one is ordered by prefix and then by the
	// whole key. Walk the run to find the slot whose level two record holds this exact key.
	for ; slot < i.keyCount; slot++ {
		prefix, recordOffset := i.readSlot(slot)
		if prefix != target {
			return 0, 0, false, false, nil
		}
		record, err := i.readRecord(recordOffset)
		if err != nil {
			return 0, 0, false, false, err
		}
		comparison := bytes.Compare(record.key, key)
		if comparison > 0 {
			return 0, 0, false, false, nil
		}
		if comparison < 0 {
			continue
		}
		// The pod holds the key. Whether a version of it falls in the queried range is a separate question.
		if !overlaps {
			return 0, 0, false, true, nil
		}
		entryOffset, delta, hit := record.newestInRange(lowDelta, highDelta)
		if !hit {
			return 0, 0, false, true, nil
		}
		return entryOffset, i.info.FirstBlock + uint64(delta), true, true, nil
	}
	return 0, 0, false, false, nil
}

// Path returns the file the index lives in.
func (i *podIndex) Path() string {
	return i.path
}

// Size returns the bytes the file occupies.
func (i *podIndex) Size() int64 {
	return i.size
}

// Delete unmaps the index and removes its file.
func (i *podIndex) Delete() error {
	if i.mapping != nil {
		if err := unix.Munmap(i.mapping); err != nil {
			return fmt.Errorf("failed to unmap pod index %s: %w", i.path, err)
		}
		i.mapping = nil
		i.level1 = nil
		i.level2 = nil
	}
	if err := os.Remove(i.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete pod index %s: %w", i.path, err)
	}
	return nil
}

// deltaRange converts an absolute block range into the pod-relative deltas a level two record is keyed by.
//
// lowDelta is exclusive and may be negative, which means the whole pod is in range. overlaps is false when
// the requested range misses the pod entirely.
func (i *podIndex) deltaRange(lowBlock uint64, highBlock uint64) (lowDelta int64, highDelta int64, overlaps bool) {
	if highBlock < i.info.FirstBlock || lowBlock >= i.info.LastBlock {
		return 0, 0, false
	}
	if highBlock > i.info.LastBlock {
		highBlock = i.info.LastBlock
	}
	//nolint:gosec // G115 - block deltas are bounded by the pod's own span
	highDelta = int64(highBlock - i.info.FirstBlock)
	lowDelta = -1
	if lowBlock >= i.info.FirstBlock {
		//nolint:gosec // G115 - block deltas are bounded by the pod's own span
		lowDelta = int64(lowBlock - i.info.FirstBlock)
	}
	return lowDelta, highDelta, true
}

// searchLevelOne returns the first slot whose key prefix is at or above target.
func (i *podIndex) searchLevelOne(target uint64) uint64 {
	keyCount := int(i.keyCount) //nolint:gosec // G115 - bounded by the level one length, checked on open
	slot := sort.Search(keyCount, func(candidate int) bool {
		//nolint:gosec // G115 - a search index is bounded by the key count
		prefix, _ := i.readSlot(uint64(candidate))
		return prefix >= target
	})
	return uint64(slot) //nolint:gosec // G115 - as above
}

// readSlot reads one level one slot. The slot must be below keyCount, which the caller guarantees.
func (i *podIndex) readSlot(slot uint64) (prefix uint64, recordOffset uint32) {
	entry := i.level1[slot*indexSlotSize:]
	return binary.BigEndian.Uint64(entry[0:8]), binary.BigEndian.Uint32(entry[8:12])
}

// indexRecord is one key's level two record: the key itself and every version of it the pod holds. Both
// alias the mapping rather than copying out of it.
type indexRecord struct {
	key      []byte
	versions []byte
}

// newestInRange returns the entry offset of the newest version whose block delta lies in (lowDelta, highDelta].
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

// readRecord returns the level two record at the given offset from the start of level two.
//
// Every length taken from the file is checked against what remains of level two before it is used to slice.
// A record is described by bytes the file itself supplies, so a corrupt file could otherwise walk off the end
// of the mapping, which faults rather than failing.
func (i *podIndex) readRecord(recordOffset uint32) (*indexRecord, error) {
	cursor := int(recordOffset)
	if cursor < 0 || cursor+indexRecordKeyLengthSize > len(i.level2) {
		return nil, fmt.Errorf("pod index %s puts a record at %d of %d level two bytes",
			i.path, recordOffset, len(i.level2))
	}

	keyLength := int(binary.BigEndian.Uint16(i.level2[cursor:]))
	cursor += indexRecordKeyLengthSize
	if cursor+keyLength+indexRecordVersionCountSize > len(i.level2) {
		return nil, fmt.Errorf("pod index %s claims a %d byte key at %d", i.path, keyLength, recordOffset)
	}
	key := i.level2[cursor : cursor+keyLength]
	cursor += keyLength

	versionCount := int(binary.BigEndian.Uint32(i.level2[cursor:]))
	cursor += indexRecordVersionCountSize
	versionBytes := versionCount * indexVersionSize
	if versionCount < 0 || cursor+versionBytes > len(i.level2) {
		return nil, fmt.Errorf("pod index %s claims %d versions at %d", i.path, versionCount, recordOffset)
	}

	return &indexRecord{key: key, versions: i.level2[cursor : cursor+versionBytes]}, nil
}

// openPodIndex maps an existing pod index file for searching.
func openPodIndex(path string) (*podIndex, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod index %s: %w", path, err)
	}
	// The mapping outlives the descriptor, so the descriptor is released here rather than being held for the
	// life of the pod.
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pod index %s: %w", path, err)
	}
	size := stat.Size()
	if size < podIndexHeaderSize {
		return nil, fmt.Errorf("pod index %s is truncated at %d bytes", path, size)
	}

	mapping, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to map pod index %s: %w", path, err)
	}

	index, err := parseMappedIndex(path, size, mapping)
	if err != nil {
		_ = unix.Munmap(mapping)
		return nil, err
	}
	return index, nil
}

// parseMappedIndex validates a mapped index's header and describes the two levels behind it.
func parseMappedIndex(path string, size int64, mapping []byte) (*podIndex, error) {
	if string(mapping[:len(podIndexMagic)]) != string(podIndexMagic) {
		return nil, fmt.Errorf("pod index %s is not an index: bad magic", path)
	}
	if version := mapping[len(podIndexMagic)]; version != podIndexFormatVersion {
		return nil, fmt.Errorf("pod index %s has format version %d, expected %d",
			path, version, podIndexFormatVersion)
	}

	firstBlock := binary.BigEndian.Uint64(mapping[9:17])
	lastBlock := binary.BigEndian.Uint64(mapping[17:25])
	keyCount := binary.BigEndian.Uint64(mapping[25:33])
	level2Start := binary.BigEndian.Uint64(mapping[33:41])

	// Level one has to hold exactly keyCount slots and level two has to start where they end, or every offset
	// the search derives from them is meaningless.
	//nolint:gosec // G115 - the header values are range checked against the file size here
	expectedLevel2Start := uint64(podIndexHeaderSize) + keyCount*indexSlotSize
	fileSize := uint64(size) //nolint:gosec // G115 - a file size is never negative
	if level2Start != expectedLevel2Start || level2Start > fileSize {
		return nil, fmt.Errorf("pod index %s puts %d keys and level two at %d of %d bytes",
			path, keyCount, level2Start, size)
	}

	return &podIndex{
		path:     path,
		info:     &PodInfo{FirstBlock: firstBlock, LastBlock: lastBlock},
		size:     size,
		keyCount: keyCount,
		mapping:  mapping,
		level1:   mapping[podIndexHeaderSize:level2Start],
		level2:   mapping[level2Start:],
	}, nil
}

// keyPrefix returns the first 8 bytes of key, zero padded on the right, as a big endian uint64.
//
// Ordering keys by this value and breaking ties on the full key is the same ordering as comparing the keys
// themselves, which is what lets level one be searched without reading any key.
func keyPrefix(key []byte) uint64 {
	var prefix uint64
	for i := 0; i < 8; i++ {
		prefix <<= 8
		if i < len(key) {
			prefix |= uint64(key[i])
		}
	}
	return prefix
}

// writePodIndex sorts the pod's entry references and writes its index, reporting how many distinct keys it
// holds so the bloom filter can be sized.
func writePodIndex(
	path string,
	refs []podEntryRef,
	firstBlock uint64,
	lastBlock uint64,
) (keys [][]byte, size int64, err error) {
	sortEntryRefs(refs)

	level1 := make([]byte, 0, len(refs)*indexSlotSize)
	level2 := make([]byte, 0, len(refs)*16)
	keys = make([][]byte, 0, len(refs)/2)

	for start := 0; start < len(refs); {
		end := start + 1
		for end < len(refs) && refs[end].prefix == refs[start].prefix &&
			bytes.Equal(refs[end].key, refs[start].key) {
			end++
		}
		if len(level2) > int(maxIndexLevel2Size) {
			return nil, 0, fmt.Errorf("pod index level two reached %d bytes, past the addressable limit",
				len(level2))
		}
		level1 = binary.BigEndian.AppendUint64(level1, refs[start].prefix)
		//nolint:gosec // G115 - level two size is bounded by the check above
		level1 = binary.BigEndian.AppendUint32(level1, uint32(len(level2)))
		level2 = appendIndexRecord(level2, refs[start:end])
		keys = append(keys, refs[start].key)
		start = end
	}

	level2Start := int64(podIndexHeaderSize) + int64(len(level1))
	header := make([]byte, 0, podIndexHeaderSize)
	header = append(header, podIndexMagic...)
	header = append(header, podIndexFormatVersion)
	header = binary.BigEndian.AppendUint64(header, firstBlock)
	header = binary.BigEndian.AppendUint64(header, lastBlock)
	header = binary.BigEndian.AppendUint64(header, uint64(len(keys)))
	//nolint:gosec // G115 - level2Start is the header plus level one, far below the int64 ceiling
	header = binary.BigEndian.AppendUint64(header, uint64(level2Start))

	if err := writeFileParts(path, header, level1, level2); err != nil {
		return nil, 0, err
	}
	return keys, int64(len(header)) + int64(len(level1)) + int64(len(level2)), nil
}

// The largest level two section a pod index may hold, since a level one slot addresses it with a uint32.
const maxIndexLevel2Size = uint64(1)<<32 - 1

// appendIndexRecord appends one key's level two record, built from that key's runs of entry references.
//
// The references are ascending by block, and a block that wrote the key more than once contributes only its
// last write, which is the one a query must see.
func appendIndexRecord(level2 []byte, refs []podEntryRef) []byte {
	key := refs[0].key
	//nolint:gosec // G115 - key length is bounded by the pod writer
	level2 = binary.BigEndian.AppendUint16(level2, uint16(len(key)))
	level2 = append(level2, key...)

	countPosition := len(level2)
	level2 = binary.BigEndian.AppendUint32(level2, 0)

	versions := uint32(0)
	for index, ref := range refs {
		if index > 0 && ref.blockDelta == refs[index-1].blockDelta {
			// Same block wrote this key again; overwrite the version just appended rather than adding one.
			binary.BigEndian.PutUint32(level2[len(level2)-4:], ref.offset)
			continue
		}
		level2 = binary.BigEndian.AppendUint32(level2, ref.blockDelta)
		level2 = binary.BigEndian.AppendUint32(level2, ref.offset)
		versions++
	}
	binary.BigEndian.PutUint32(level2[countPosition:], versions)
	return level2
}

// sortEntryRefs orders references by key prefix, then whole key, then block, then write order.
//
// It buckets by the high bits of the prefix and sorts the buckets in parallel. Because level one is ordered by
// prefix, the buckets are already in output order and concatenate without a merge.
func sortEntryRefs(refs []podEntryRef) {
	if len(refs) < 1<<16 {
		sort.Slice(refs, func(a int, b int) bool { return lessEntryRef(refs[a], refs[b]) })
		return
	}

	bucketCount := 1
	for bucketCount < runtime.NumCPU()*4 {
		bucketCount *= 2
	}
	shift := 64
	for size := bucketCount; size > 1; size >>= 1 {
		shift--
	}

	buckets := make([][]podEntryRef, bucketCount)
	for _, ref := range refs {
		bucket := ref.prefix >> shift
		buckets[bucket] = append(buckets[bucket], ref)
	}

	var group sync.WaitGroup
	for index := range buckets {
		group.Add(1)
		go func(bucket []podEntryRef) {
			defer group.Done()
			sort.Slice(bucket, func(a int, b int) bool { return lessEntryRef(bucket[a], bucket[b]) })
		}(buckets[index])
	}
	group.Wait()

	cursor := 0
	for _, bucket := range buckets {
		cursor += copy(refs[cursor:], bucket)
	}
}

// lessEntryRef orders two entry references.
func lessEntryRef(a podEntryRef, b podEntryRef) bool {
	if a.prefix != b.prefix {
		return a.prefix < b.prefix
	}
	if comparison := bytes.Compare(a.key, b.key); comparison != 0 {
		return comparison < 0
	}
	if a.blockDelta != b.blockDelta {
		return a.blockDelta < b.blockDelta
	}
	return a.offset < b.offset
}
