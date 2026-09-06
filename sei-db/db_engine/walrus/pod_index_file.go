package walrus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

var _ PodIndex = (*podIndex)(nil)

// podIndex searches a pod's index, which is two files.
//
// The hash index is a sorted array of one 32 bit key hash per distinct key, and it is what a search walks.
// The version index holds a pointer per key, positionally matching the hash index, and behind them the key
// records: the whole key and every version of it the pod holds. A search binary searches the hash index and
// resolves the entry it lands on against the version index once.
//
// Splitting them is what lets the searched half stay mapped: it is a twentieth the size of the records, and
// it is read at every probe where the records are read once. Sharing one file would let pages used once
// evict the pages used at every probe.
//
// Ordering by a hash rather than by the key removes the index's dependence on what keys look like. Under a
// key prefix ordering, keys sharing a prefix form one run, which real keys do by construction: every storage
// slot of one contract shares its leading bytes. Under a hash ordering a run longer than one entry is a
// collision, which is rare and unrelated to what the keys contain.
type podIndex struct {
	// The pod's own directory, which both files live in.
	directory string

	info *PodInfo

	// The searched half: one hash per distinct key, mapped.
	hashes *podHashIndex

	// The dereferenced half: a pointer and a key record per distinct key, read on a hit.
	versions *podVersionIndex
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
	if i.hashes.keyCount == 0 {
		return 0, 0, false, false, nil
	}

	target := indexHash(podKeyHash(i.hashes.salt, key))
	slot := i.hashes.search(target)

	// Entries sharing a hash form a contiguous run, ordered within it by key. Walk the run to find the entry
	// whose record holds this exact key. A run longer than one entry means two keys collided.
	for ; slot < i.hashes.keyCount; slot++ {
		if i.hashes.readHash(slot) != target {
			return 0, 0, false, false, nil
		}
		record, err := i.versions.readRecord(slot)
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

// Path returns the pod directory the index's files live in.
func (i *podIndex) Path() string {
	return i.directory
}

// Size returns the bytes the index's files occupy together.
func (i *podIndex) Size() int64 {
	return i.hashes.Size() + i.versions.Size()
}

// Delete releases the index's mapping and removes its files.
func (i *podIndex) Delete() error {
	var problems []error
	if err := i.hashes.Delete(); err != nil {
		problems = append(problems, err)
	}
	if err := i.versions.Delete(); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// deltaRange converts an absolute block range into the pod-relative deltas a key record is keyed by.
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

// openPodIndex opens the two index files in a pod's directory.
func openPodIndex(directory string) (*podIndex, error) {
	hashes, err := openPodHashIndex(filepath.Join(directory, podHashIndexFileName))
	if err != nil {
		return nil, err
	}
	versions, err := openPodVersionIndex(filepath.Join(directory, podVersionIndexFileName))
	if err != nil {
		_ = hashes.release()
		return nil, err
	}

	// A pointer is only meaningful at the position the hash index landed on, so the two files describing
	// different numbers of keys means one of them is not the other's.
	if hashes.keyCount != versions.keyCount {
		_ = hashes.release()
		return nil, fmt.Errorf("pod index %s holds %d hashes and %d pointers",
			directory, hashes.keyCount, versions.keyCount)
	}

	return &podIndex{
		directory: directory,
		info:      hashes.info,
		hashes:    hashes,
		versions:  versions,
	}, nil
}

// writePodIndex sorts the pod's entry references and writes both index files, returning the hash of every
// distinct key so the bloom filter can be built without hashing them again.
func writePodIndex(
	directory string,
	refs []podEntryRef,
	firstBlock uint64,
	lastBlock uint64,
	salt uint64,
) (keyHashes []uint64, hashSize int64, versionSize int64, err error) {
	sortEntryRefs(refs)

	hashes := make([]byte, 0, len(refs)*indexHashSize)
	pointers := make([]byte, 0, len(refs)*indexPointerSize)
	records := make([]byte, 0, len(refs)*16)
	keyHashes = make([]uint64, 0, len(refs)/2)

	for start := 0; start < len(refs); {
		end := start + 1
		for end < len(refs) && refs[end].hash == refs[start].hash &&
			bytes.Equal(refs[end].key, refs[start].key) {
			end++
		}
		hashes = binary.BigEndian.AppendUint32(hashes, indexHash(refs[start].hash))
		pointers = binary.BigEndian.AppendUint64(pointers, uint64(len(records)))
		records = appendIndexRecord(records, refs[start:end])
		keyHashes = append(keyHashes, refs[start].hash)
		start = end
	}

	keyCount := uint64(len(keyHashes))
	hashSize, err = writePodHashIndex(
		filepath.Join(directory, podHashIndexFileName), hashes, keyCount, firstBlock, lastBlock, salt)
	if err != nil {
		return nil, 0, 0, err
	}
	versionSize, err = writePodVersionIndex(
		filepath.Join(directory, podVersionIndexFileName), pointers, records, keyCount)
	if err != nil {
		return nil, 0, 0, err
	}
	return keyHashes, hashSize, versionSize, nil
}

// appendIndexRecord appends one key record, built from that key's run of entry references.
//
// The references are ascending by block, and a block that wrote the key more than once contributes only its
// last write, which is the one a query must see.
func appendIndexRecord(records []byte, refs []podEntryRef) []byte {
	key := refs[0].key
	//nolint:gosec // G115 - key length is bounded by the pod writer
	records = binary.BigEndian.AppendUint16(records, uint16(len(key)))
	records = append(records, key...)

	countPosition := len(records)
	records = binary.BigEndian.AppendUint32(records, 0)

	versions := uint32(0)
	for index, ref := range refs {
		if index > 0 && ref.blockDelta == refs[index-1].blockDelta {
			// Same block wrote this key again; overwrite the version just appended rather than adding one.
			binary.BigEndian.PutUint32(records[len(records)-4:], ref.offset)
			continue
		}
		records = binary.BigEndian.AppendUint32(records, ref.blockDelta)
		records = binary.BigEndian.AppendUint32(records, ref.offset)
		versions++
	}
	binary.BigEndian.PutUint32(records[countPosition:], versions)
	return records
}

// sortEntryRefs orders references by index hash, then whole key, then block, then write order.
//
// It buckets by the high bits of the hash and sorts the buckets in parallel. Because the entries are ordered
// by hash, the buckets are already in output order and concatenate without a merge. The buckets come out
// even whatever the keys look like, which is the property a prefix of the key does not have: every storage
// key of one contract shares its leading bytes, so bucketing on those puts a contract in one bucket.
func sortEntryRefs(refs []podEntryRef) {
	if len(refs) < 1<<16 {
		sort.Slice(refs, func(a int, b int) bool { return lessEntryRef(refs[a], refs[b]) })
		return
	}

	bucketCount := 1
	for bucketCount < runtime.NumCPU()*4 {
		bucketCount *= 2
	}
	shift := 32
	for size := bucketCount; size > 1; size >>= 1 {
		shift--
	}

	buckets := make([][]podEntryRef, bucketCount)
	for _, ref := range refs {
		bucket := indexHash(ref.hash) >> shift
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
//
// It orders on the truncated hash the index stores rather than the whole one, so that entries sharing an
// index hash are ordered by key. That is what lets a search stop walking a run of collisions as soon as it
// passes the key it wants.
func lessEntryRef(a podEntryRef, b podEntryRef) bool {
	if indexHash(a.hash) != indexHash(b.hash) {
		return indexHash(a.hash) < indexHash(b.hash)
	}
	if comparison := bytes.Compare(a.key, b.key); comparison != 0 {
		return comparison < 0
	}
	if a.blockDelta != b.blockDelta {
		return a.blockDelta < b.blockDelta
	}
	return a.offset < b.offset
}
