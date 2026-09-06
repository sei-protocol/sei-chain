package walrus

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// The first bytes of every pod bloom filter file, identifying the format.
var podBloomMagic = []byte("WALRSBLM")

// The version of the pod bloom filter layout, validated on read so a filter written by an incompatible build
// is refused rather than misparsed.
const podBloomFormatVersion = byte(2)

// The size of a bloom filter header: 8 byte magic, 1 byte version, 8 byte bit count, 1 byte hash count,
// 8 byte salt.
const podBloomHeaderSize = 26

// The largest number of hash functions a filter may use. The count is stored in one byte, and a filter
// needing more than this is one whose false positive rate should have been relaxed instead.
const maxBloomHashCount = 255

var _ PodBloom = (*podBloom)(nil)

// podBloom probes a pod's bloom filter.
//
// The filter is memory mapped rather than read into memory. Across an archive the filters run to terabytes,
// far past what a process could hold, so the pages a probe touches are paged in on demand and the operating
// system evicts them under pressure. Mapping also keeps MayContain free of an error return, since a mapped
// immutable file has nothing left that can fail, and costs no file descriptor: the mapping outlives the
// descriptor it was created from.
//
// The mapping is released by Delete, which is why the catalog will not delete a pod a query still references:
// unlinking a file underneath a reader is safe, but unmapping memory it is still reading is not.
type podBloom struct {
	path      string
	size      int64
	bitCount  uint64
	hashCount uint8
	salt      uint64

	// The whole file as mapped, which is what Munmap has to be given back.
	mapping []byte

	// The bit array within the mapping, past the header.
	bits []byte
}

// MayContain reports whether the pod may hold key.
func (b *podBloom) MayContain(key []byte) bool {
	first, second := bloomPositions(podKeyHash(b.salt, key))
	for probe := uint8(0); probe < b.hashCount; probe++ {
		position := (first + uint64(probe)*second) % b.bitCount
		if b.bits[position/8]&(1<<(position%8)) == 0 {
			return false
		}
	}
	return true
}

// Path returns the file the filter lives in.
func (b *podBloom) Path() string {
	return b.path
}

// Size returns the bytes the file occupies.
func (b *podBloom) Size() int64 {
	return b.size
}

// Delete unmaps the filter and removes its file.
func (b *podBloom) Delete() error {
	if b.mapping != nil {
		if err := unix.Munmap(b.mapping); err != nil {
			return fmt.Errorf("failed to unmap pod bloom filter %s: %w", b.path, err)
		}
		b.mapping = nil
		b.bits = nil
	}
	if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete pod bloom filter %s: %w", b.path, err)
	}
	return nil
}

// openPodBloom maps an existing bloom filter file.
func openPodBloom(path string) (*podBloom, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to open pod bloom filter %s: %w", path, err)
	}
	// The mapping outlives the descriptor, so the descriptor is released here rather than being held for the
	// life of the pod. An archive holds hundreds of thousands of pods; three descriptors each would not fit.
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pod bloom filter %s: %w", path, err)
	}
	size := stat.Size()
	if size < podBloomHeaderSize {
		return nil, fmt.Errorf("pod bloom filter %s is truncated at %d bytes", path, size)
	}

	mapping, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to map pod bloom filter %s: %w", path, err)
	}

	filter, err := parseMappedBloom(path, size, mapping)
	if err != nil {
		_ = unix.Munmap(mapping)
		return nil, err
	}
	return filter, nil
}

// parseMappedBloom validates a mapped filter's header and describes the bits behind it.
func parseMappedBloom(path string, size int64, mapping []byte) (*podBloom, error) {
	if string(mapping[:len(podBloomMagic)]) != string(podBloomMagic) {
		return nil, fmt.Errorf("pod bloom filter %s is not a filter: bad magic", path)
	}
	if version := mapping[len(podBloomMagic)]; version != podBloomFormatVersion {
		return nil, fmt.Errorf("pod bloom filter %s has format version %d, expected %d",
			path, version, podBloomFormatVersion)
	}

	bitCount := binary.BigEndian.Uint64(mapping[9:17])
	hashCount := mapping[17]
	salt := binary.BigEndian.Uint64(mapping[18:26])
	if bitCount == 0 || hashCount == 0 {
		return nil, fmt.Errorf("pod bloom filter %s declares %d bits and %d hashes", path, bitCount, hashCount)
	}
	bits := mapping[podBloomHeaderSize:]
	if uint64(len(bits)) != (bitCount+7)/8 {
		return nil, fmt.Errorf("pod bloom filter %s holds %d bytes for %d bits", path, len(bits), bitCount)
	}

	return &podBloom{
		path:      path,
		size:      size,
		bitCount:  bitCount,
		hashCount: hashCount,
		salt:      salt,
		mapping:   mapping,
		bits:      bits,
	}, nil
}

// writePodBloom builds and writes a bloom filter over the hashes of a pod's distinct keys, at the configured
// false positive rate.
//
// It takes hashes rather than keys because the index has already computed them under this pod's salt, and
// because the filter has no use for a key beyond its hash.
func writePodBloom(path string, keyHashes []uint64, falsePositiveRate float64, salt uint64) (
	size int64,
	err error,
) {
	bitCount, hashCount := bloomSizing(uint64(len(keyHashes)), falsePositiveRate)
	bits := make([]byte, (bitCount+7)/8)

	for _, hash := range keyHashes {
		first, second := bloomPositions(hash)
		for probe := uint8(0); probe < hashCount; probe++ {
			position := (first + uint64(probe)*second) % bitCount
			bits[position/8] |= 1 << (position % 8)
		}
	}

	header := make([]byte, 0, podBloomHeaderSize)
	header = append(header, podBloomMagic...)
	header = append(header, podBloomFormatVersion)
	header = binary.BigEndian.AppendUint64(header, bitCount)
	header = append(header, hashCount)
	header = binary.BigEndian.AppendUint64(header, salt)

	if err := writeFileParts(path, header, bits); err != nil {
		return 0, err
	}
	return int64(len(header)) + int64(len(bits)), nil
}

// bloomPositions returns the two values a key's bit positions are derived from.
//
// Positions come from one hash expanded by the Kirsch-Mitzenmacher construction, so a probe hashes the key
// once regardless of how many bits it has to test. The hash is salted per pod, which is what stops a key
// chosen to set the same bits as another from doing so in every pod it is written to.
func bloomPositions(hash uint64) (first uint64, second uint64) {
	// The step is forced odd so that walking by it visits distinct positions rather than cycling early on a
	// bit count it shares a factor with. It is a mix of the first value rather than a second hash of the key,
	// which keeps a probe to one pass over the key and no allocation.
	return hash, mix64(hash) | 1
}

// mix64 is the SplitMix64 finalizer, used to derive a well distributed second value from the first.
func mix64(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	value ^= value >> 31
	return value
}

// bloomSizing returns the bit array size and hash count that hold keyCount keys at the given false positive
// rate.
//
// A filter for zero keys still gets one bit and one hash, so probing it is the same code path as probing any
// other.
func bloomSizing(keyCount uint64, falsePositiveRate float64) (bitCount uint64, hashCount uint8) {
	if keyCount == 0 {
		return 1, 1
	}

	ln2 := math.Ln2
	bits := -float64(keyCount) * math.Log(falsePositiveRate) / (ln2 * ln2)
	bitCount = uint64(math.Ceil(bits))
	if bitCount == 0 {
		bitCount = 1
	}

	hashes := math.Round(float64(bitCount) / float64(keyCount) * ln2)
	if hashes < 1 {
		hashes = 1
	}
	if hashes > maxBloomHashCount {
		hashes = maxBloomHashCount
	}
	return bitCount, uint8(hashes)
}

// writeFileParts writes the concatenation of parts to path, durably.
//
// It does not rename anything into place: a pod's files are written into a directory the builder renames as
// a whole, so that an interrupted build leaves no pod a later open could mistake for a complete one.
func writeFileParts(path string, parts ...[]byte) error {
	temporary := path
	file, err := os.Create(temporary) //nolint:gosec // path is derived from a validated directory
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", temporary, err)
	}

	writer := bufio.NewWriterSize(file, 1<<20)
	for _, part := range parts {
		if _, err := writer.Write(part); err != nil {
			_ = file.Close()
			return fmt.Errorf("failed to write %s: %w", temporary, err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to flush %s: %w", temporary, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to sync %s: %w", temporary, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", temporary, err)
	}
	return nil
}
