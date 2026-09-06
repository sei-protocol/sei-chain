package walrus

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"

	"golang.org/x/sys/unix"
)

// The first bytes of every pod hash index file, identifying the format.
var podHashIndexMagic = []byte("WALRSHSH")

// The version of the pod hash index layout, validated on read so an index written by an incompatible build is
// refused rather than misparsed.
const podHashIndexFormatVersion = byte(1)

// The size of a pod hash index header: 8 byte magic, 1 byte version, 8 byte first block, 8 byte last block,
// 8 byte key count, 8 byte salt.
const podHashIndexHeaderSize = 41

// The size of one hash index entry.
const indexHashSize = 4

// podHashIndex is the searched half of a pod's index: one 32 bit key hash per distinct key, ascending.
//
// It holds nothing else. A search reads only this file, and resolves the entry it lands on against the
// version index afterwards. Keeping the record offsets out of it is what makes it small enough to stay
// mapped across an archive far larger than memory, which is worth more than the one indirection it costs:
// a search touches this file about fourteen times and the version index once.
//
// The file is memory mapped rather than read through. A search is a walk of single entries at scattered
// offsets, and reading those through the file interface would cost a syscall apiece to move bytes the kernel
// has already paged in.
//
// Mapping is not residency. The kernel pages in what a search touches and evicts under pressure.
//
// The mapping is released by Delete, which is why the catalog will not delete a pod a query still
// references: unlinking a file underneath a reader is safe, but unmapping memory it is still reading is not.
type podHashIndex struct {
	path     string
	info     *PodInfo
	size     int64
	keyCount uint64
	salt     uint64

	// The whole file as mapped, which is what Munmap has to be given back.
	mapping []byte

	// The hash array within the mapping, past the header.
	hashes []byte
}

// search returns the first entry whose hash is at or above target.
//
// Entries sharing a hash form a contiguous run, so a caller resolves the run against the version index until
// it finds the key it wants or passes it.
func (h *podHashIndex) search(target uint32) uint64 {
	keyCount := int(h.keyCount) //nolint:gosec // G115 - bounded by the hash array length, checked on open
	slot := sort.Search(keyCount, func(candidate int) bool {
		//nolint:gosec // G115 - a search index is bounded by the key count
		return h.readHash(uint64(candidate)) >= target
	})
	return uint64(slot) //nolint:gosec // G115 - as above
}

// readHash reads one entry. The slot must be below keyCount, which the caller guarantees.
func (h *podHashIndex) readHash(slot uint64) uint32 {
	return binary.BigEndian.Uint32(h.hashes[slot*indexHashSize:])
}

// Path returns the file the hash index lives in.
func (h *podHashIndex) Path() string {
	return h.path
}

// Size returns the bytes the file occupies.
func (h *podHashIndex) Size() int64 {
	return h.size
}

// Delete unmaps the hash index and removes its file.
func (h *podHashIndex) Delete() error {
	if err := h.release(); err != nil {
		return err
	}
	if err := os.Remove(h.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete pod hash index %s: %w", h.path, err)
	}
	return nil
}

// release gives the mapping back without touching the file, for the paths that abandon a hash index they
// have already mapped rather than deleting one they are done with.
func (h *podHashIndex) release() error {
	if h.mapping == nil {
		return nil
	}
	if err := unix.Munmap(h.mapping); err != nil {
		return fmt.Errorf("failed to unmap pod hash index %s: %w", h.path, err)
	}
	h.mapping = nil
	h.hashes = nil
	return nil
}

// openPodHashIndex maps an existing pod hash index file for searching.
func openPodHashIndex(path string) (*podHashIndex, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod hash index %s: %w", path, err)
	}
	// The mapping outlives the descriptor, so the descriptor is released here rather than being held for the
	// life of the pod.
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pod hash index %s: %w", path, err)
	}
	size := stat.Size()
	if size < podHashIndexHeaderSize {
		return nil, fmt.Errorf("pod hash index %s is truncated at %d bytes", path, size)
	}

	mapping, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to map pod hash index %s: %w", path, err)
	}

	index, err := parseMappedHashIndex(path, size, mapping)
	if err != nil {
		_ = unix.Munmap(mapping)
		return nil, err
	}
	return index, nil
}

// parseMappedHashIndex validates a mapped hash index's header and describes the entries behind it.
func parseMappedHashIndex(path string, size int64, mapping []byte) (*podHashIndex, error) {
	if string(mapping[:len(podHashIndexMagic)]) != string(podHashIndexMagic) {
		return nil, fmt.Errorf("pod hash index %s is not a hash index: bad magic", path)
	}
	if version := mapping[len(podHashIndexMagic)]; version != podHashIndexFormatVersion {
		return nil, fmt.Errorf("pod hash index %s has format version %d, expected %d",
			path, version, podHashIndexFormatVersion)
	}

	firstBlock := binary.BigEndian.Uint64(mapping[9:17])
	lastBlock := binary.BigEndian.Uint64(mapping[17:25])
	keyCount := binary.BigEndian.Uint64(mapping[25:33])
	salt := binary.BigEndian.Uint64(mapping[33:41])

	// The entries have to fill the file exactly, or a search derives slots the file does not hold.
	hashes := mapping[podHashIndexHeaderSize:]
	if uint64(len(hashes)) != keyCount*indexHashSize {
		return nil, fmt.Errorf("pod hash index %s holds %d bytes for %d keys", path, len(hashes), keyCount)
	}

	return &podHashIndex{
		path:     path,
		info:     &PodInfo{FirstBlock: firstBlock, LastBlock: lastBlock},
		size:     size,
		keyCount: keyCount,
		salt:     salt,
		mapping:  mapping,
		hashes:   hashes,
	}, nil
}

// writePodHashIndex writes a pod's hash index, whose entries must already be ascending.
func writePodHashIndex(
	path string,
	hashes []byte,
	keyCount uint64,
	firstBlock uint64,
	lastBlock uint64,
	salt uint64,
) (size int64, err error) {
	header := make([]byte, 0, podHashIndexHeaderSize)
	header = append(header, podHashIndexMagic...)
	header = append(header, podHashIndexFormatVersion)
	header = binary.BigEndian.AppendUint64(header, firstBlock)
	header = binary.BigEndian.AppendUint64(header, lastBlock)
	header = binary.BigEndian.AppendUint64(header, keyCount)
	header = binary.BigEndian.AppendUint64(header, salt)

	if err := writeFileParts(path, header, hashes); err != nil {
		return 0, err
	}
	return int64(len(header)) + int64(len(hashes)), nil
}
