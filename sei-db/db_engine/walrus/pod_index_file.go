package walrus

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
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

// The number of level one slots read in a single probe. A binary search converges on a small span, so pulling
// a run of neighbours costs one syscall instead of several.
const indexProbeSlots = 8

var _ PodIndex = (*podIndex)(nil)

// podIndex searches a pod index file.
//
// It holds the header and nothing else from the file. A search opens the file, walks level one by reading
// twelve byte slots, and dereferences into level two once to confirm the full key. Searches only happen when
// a bloom filter has already failed to rule the pod out.
type podIndex struct {
	path        string
	info        *PodInfo
	size        int64
	keyCount    uint64
	level2Start int64
}

// FindNewest returns where the newest version of key written in (lowBlock, highBlock] lives.
func (i *podIndex) FindNewest(key []byte, lowBlock uint64, highBlock uint64) (
	offset uint32,
	blockNumber uint64,
	found bool,
	err error,
) {
	lowDelta, highDelta, overlaps := i.deltaRange(lowBlock, highBlock)
	if !overlaps || i.keyCount == 0 {
		return 0, 0, false, nil
	}

	file, err := os.Open(i.path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return 0, 0, false, fmt.Errorf("failed to open pod index %s: %w", i.path, err)
	}
	defer func() { _ = file.Close() }()

	slot, err := i.searchLevelOne(file, keyPrefix(key))
	if err != nil {
		return 0, 0, false, err
	}

	// Slots sharing a key prefix form a contiguous run, since level one is ordered by prefix and then by the
	// whole key. Walk the run to find the slot whose level two record holds this exact key.
	target := keyPrefix(key)
	for ; slot < i.keyCount; slot++ {
		prefix, recordOffset, err := i.readSlot(file, slot)
		if err != nil {
			return 0, 0, false, err
		}
		if prefix != target {
			return 0, 0, false, nil
		}
		record, err := i.readRecord(file, recordOffset)
		if err != nil {
			return 0, 0, false, err
		}
		comparison := bytes.Compare(record.key, key)
		if comparison > 0 {
			return 0, 0, false, nil
		}
		if comparison < 0 {
			continue
		}
		entryOffset, delta, hit := record.newestInRange(lowDelta, highDelta)
		if !hit {
			return 0, 0, false, nil
		}
		return entryOffset, i.info.FirstBlock + uint64(delta), true, nil
	}
	return 0, 0, false, nil
}

// Path returns the file the index lives in.
func (i *podIndex) Path() string {
	return i.path
}

// Size returns the bytes the file occupies.
func (i *podIndex) Size() int64 {
	return i.size
}

// Delete removes the file.
func (i *podIndex) Delete() error {
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
func (i *podIndex) searchLevelOne(file *os.File, target uint64) (uint64, error) {
	var searchErr error
	keyCount := int(i.keyCount) //nolint:gosec // G115 - bounded by the entries a pod can hold
	slot := sort.Search(keyCount, func(candidate int) bool {
		if searchErr != nil {
			return true
		}
		//nolint:gosec // G115 - a search index is bounded by the key count
		prefix, _, err := i.readSlot(file, uint64(candidate))
		if err != nil {
			searchErr = err
			return true
		}
		return prefix >= target
	})
	if searchErr != nil {
		return 0, searchErr
	}
	return uint64(slot), nil //nolint:gosec // G115 - as above
}

// readSlot reads one level one slot.
func (i *podIndex) readSlot(file *os.File, slot uint64) (prefix uint64, recordOffset uint32, err error) {
	//nolint:gosec // G115 - slot is bounded by keyCount, which the header validated
	position := int64(podIndexHeaderSize) + int64(slot)*indexSlotSize
	buffer := make([]byte, indexSlotSize)
	if _, err := file.ReadAt(buffer, position); err != nil {
		return 0, 0, fmt.Errorf("failed to read index slot %d of %s: %w", slot, i.path, err)
	}
	return binary.BigEndian.Uint64(buffer[0:8]), binary.BigEndian.Uint32(buffer[8:12]), nil
}

// indexRecord is one key's level two record: the key itself and every version of it the pod holds.
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

// readRecord reads the level two record at the given offset from the start of level two.
func (i *podIndex) readRecord(file *os.File, recordOffset uint32) (*indexRecord, error) {
	position := i.level2Start + int64(recordOffset)
	header := make([]byte, 2)
	if _, err := file.ReadAt(header, position); err != nil {
		return nil, fmt.Errorf("failed to read index record at %d of %s: %w", recordOffset, i.path, err)
	}
	keyLength := int(binary.BigEndian.Uint16(header))

	body := make([]byte, keyLength+4)
	if _, err := file.ReadAt(body, position+2); err != nil {
		return nil, fmt.Errorf("failed to read index record key at %d of %s: %w", recordOffset, i.path, err)
	}
	versionCount := binary.BigEndian.Uint32(body[keyLength:])
	size := uint64(i.size) //nolint:gosec // G115 - a file size is never negative
	if uint64(versionCount)*indexVersionSize > size {
		return nil, fmt.Errorf("index record at %d of %s claims %d versions",
			recordOffset, i.path, versionCount)
	}

	versions := make([]byte, int(versionCount)*indexVersionSize)
	if _, err := file.ReadAt(versions, position+2+int64(keyLength)+4); err != nil {
		return nil, fmt.Errorf("failed to read index versions at %d of %s: %w", recordOffset, i.path, err)
	}
	return &indexRecord{key: body[:keyLength], versions: versions}, nil
}

// openPodIndex returns a searcher over an existing pod index file.
func openPodIndex(path string) (*podIndex, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod index %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pod index %s: %w", path, err)
	}

	header := make([]byte, podIndexHeaderSize)
	if _, err := file.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("failed to read pod index header from %s: %w", path, err)
	}
	if string(header[:len(podIndexMagic)]) != string(podIndexMagic) {
		return nil, fmt.Errorf("pod index %s is not an index: bad magic", path)
	}
	if version := header[len(podIndexMagic)]; version != podIndexFormatVersion {
		return nil, fmt.Errorf("pod index %s has format version %d, expected %d",
			path, version, podIndexFormatVersion)
	}

	firstBlock := binary.BigEndian.Uint64(header[9:17])
	lastBlock := binary.BigEndian.Uint64(header[17:25])
	keyCount := binary.BigEndian.Uint64(header[25:33])
	//nolint:gosec // G115 - the offset is validated against the file size below
	level2Start := int64(binary.BigEndian.Uint64(header[33:41]))
	if level2Start < podIndexHeaderSize || level2Start > stat.Size() {
		return nil, fmt.Errorf("pod index %s puts level two at %d of %d bytes", path, level2Start, stat.Size())
	}

	return &podIndex{
		path:        path,
		info:        &PodInfo{FirstBlock: firstBlock, LastBlock: lastBlock},
		size:        stat.Size(),
		keyCount:    keyCount,
		level2Start: level2Start,
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
