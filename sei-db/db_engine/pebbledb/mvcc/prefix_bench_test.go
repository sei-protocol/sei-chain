package mvcc

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
	_ "github.com/cockroachdb/pebble/v2/sstable/block" // for CompressionProfile
)

// VersionIndex maintains key -> sorted versions mapping
type VersionIndex struct {
	mu   sync.RWMutex
	data map[string][]int64
}

func NewVersionIndex() *VersionIndex {
	return &VersionIndex{data: make(map[string][]int64)}
}

func (idx *VersionIndex) Add(key []byte, version int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	keyStr := string(key)
	versions := idx.data[keyStr]
	// Insert in sorted order
	i := sort.Search(len(versions), func(i int) bool { return versions[i] >= version })
	if i < len(versions) && versions[i] == version {
		return // Already exists
	}
	versions = append(versions, 0)
	copy(versions[i+1:], versions[i:])
	versions[i] = version
	idx.data[keyStr] = versions
}

func (idx *VersionIndex) FindLatestLE(key []byte, targetVersion int64) int64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	versions := idx.data[string(key)]
	if len(versions) == 0 {
		return 0
	}
	// Binary search for largest version <= targetVersion
	i := sort.Search(len(versions), func(i int) bool { return versions[i] > targetVersion })
	if i == 0 {
		return 0
	}
	return versions[i-1]
}

// Version-Prefixed Key Encoding: <8-byte-big-endian-version><key>
// No store key prefix, no suffix encoding - just version || key

func encodeVersionPrefix(version int64, key []byte) []byte {
	buf := make([]byte, 8+len(key))
	binary.BigEndian.PutUint64(buf[:8], uint64(version))
	copy(buf[8:], key)
	return buf
}

func decodeVersionPrefix(data []byte) (version int64, key []byte) {
	if len(data) < 8 {
		return 0, nil
	}
	version = int64(binary.BigEndian.Uint64(data[:8]))
	key = data[8:]
	return version, key
}

// openPrefixDB opens a PebbleDB with DefaultComparer for version-prefixed keys
func openPrefixDB(dir string) (*pebble.DB, error) {
	cache := pebble.NewCache(1024 * 1024 * 32) // 32MB cache
	defer cache.Unref()

	opts := &pebble.Options{
		Cache:                       cache,
		Comparer:                    pebble.DefaultComparer, // Simple lexicographic
		DisableWAL:                  false,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64MB
		MaxOpenFiles:                16384,
		MemTableSize:                64 << 20, // 64MB
		MemTableStopWritesThreshold: 4,
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
	}

	// Configure L0
	opts.Levels[0].BlockSize = 32 << 10       // 32KB
	opts.Levels[0].IndexBlockSize = 256 << 10 // 256KB
	opts.Levels[0].FilterPolicy = bloom.FilterPolicy(10)
	opts.Levels[0].FilterType = pebble.TableFilter
	opts.Levels[0].Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
	opts.Levels[0].EnsureL0Defaults()

	// Configure L1+ levels
	for i := 1; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32KB
		l.IndexBlockSize = 256 << 10 // 256KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		l.Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
		l.EnsureL1PlusDefaults(&opts.Levels[i-1])
	}

	// Disable bloom at bottom level
	opts.Levels[6].FilterPolicy = nil

	return pebble.Open(dir, opts)
}

// BenchmarkPrefixWrite benchmarks write throughput with version-prefixed keys
func BenchmarkPrefixWrite(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-prefix-write-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Pre-generate keys
	keys := make([][]byte, 10000)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100) // 100-byte values
	rand.Read(value)

	b.ResetTimer()
	b.ReportAllocs()

	version := int64(1)
	for i := 0; i < b.N; i++ {
		batch := db.NewBatch()
		// Write 1000 keys per batch (simulating a block commit)
		for j := 0; j < 1000; j++ {
			key := keys[j%len(keys)]
			encodedKey := encodeVersionPrefix(version, key)
			if err := batch.Set(encodedKey, value, nil); err != nil {
				b.Fatal(err)
			}
		}
		if err := batch.Commit(pebble.NoSync); err != nil {
			b.Fatal(err)
		}
		batch.Close()
		version++
	}

	b.StopTimer()
	b.ReportMetric(float64(1000), "keys/op")
}

// BenchmarkPrefixRead benchmarks read latency with version-prefixed keys (naive linear scan)
func BenchmarkPrefixRead(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-prefix-read-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	numKeys := 10000
	maxVersion := int64(100)
	keys := make([][]byte, numKeys)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100)
	rand.Read(value)

	// Write data - each key written at ~10 random versions
	r := rand.New(rand.NewSource(42))
	for _, key := range keys {
		for j := 0; j < 10; j++ {
			v := int64(r.Intn(int(maxVersion)) + 1)
			encodedKey := encodeVersionPrefix(v, key)
			db.Set(encodedKey, value, pebble.NoSync)
		}
	}
	db.Flush()

	b.ResetTimer()
	b.ReportAllocs()

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < b.N; i++ {
		keyIdx := r.Intn(numKeys)
		targetVersion := int64(r.Intn(int(maxVersion)) + 1)
		key := keys[keyIdx]

		// Naive: linear scan from targetVersion down
		for v := targetVersion; v >= 1; v-- {
			encodedKey := encodeVersionPrefix(v, key)
			val, closer, err := db.Get(encodedKey)
			if err == nil {
				_ = val
				closer.Close()
				break
			}
		}
	}
}

// BenchmarkPrefixReadBinarySearch uses binary search to find the version
func BenchmarkPrefixReadBinarySearch(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-prefix-read-bs-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	numKeys := 10000
	maxVersion := int64(100)
	keys := make([][]byte, numKeys)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100)
	rand.Read(value)

	r := rand.New(rand.NewSource(42))
	for _, key := range keys {
		for j := 0; j < 10; j++ {
			v := int64(r.Intn(int(maxVersion)) + 1)
			encodedKey := encodeVersionPrefix(v, key)
			db.Set(encodedKey, value, pebble.NoSync)
		}
	}
	db.Flush()

	b.ResetTimer()
	b.ReportAllocs()

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < b.N; i++ {
		keyIdx := r.Intn(numKeys)
		targetVersion := int64(r.Intn(int(maxVersion)) + 1)
		key := keys[keyIdx]

		// Binary search: find largest version <= targetVersion where key exists
		lo, hi := int64(1), targetVersion
		var foundVersion int64 = 0

		for lo <= hi {
			mid := (lo + hi) / 2
			encodedKey := encodeVersionPrefix(mid, key)
			_, closer, err := db.Get(encodedKey)
			if err == nil {
				closer.Close()
				foundVersion = mid
				lo = mid + 1 // Look for higher version
			} else {
				hi = mid - 1
			}
		}

		// If binary search found a version, also check versions between foundVersion and targetVersion
		if foundVersion > 0 && foundVersion < targetVersion {
			for v := targetVersion; v > foundVersion; v-- {
				encodedKey := encodeVersionPrefix(v, key)
				val, closer, err := db.Get(encodedKey)
				if err == nil {
					_ = val
					closer.Close()
					break
				}
			}
		}
	}
}

// BenchmarkPrefixReadIterator uses a single iterator with SeekGE
func BenchmarkPrefixReadIterator(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-prefix-read-iter-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	numKeys := 10000
	maxVersion := int64(100)
	keys := make([][]byte, numKeys)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100)
	rand.Read(value)

	r := rand.New(rand.NewSource(42))
	for _, key := range keys {
		for j := 0; j < 10; j++ {
			v := int64(r.Intn(int(maxVersion)) + 1)
			encodedKey := encodeVersionPrefix(v, key)
			db.Set(encodedKey, value, pebble.NoSync)
		}
	}
	db.Flush()

	b.ResetTimer()
	b.ReportAllocs()

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < b.N; i++ {
		keyIdx := r.Intn(numKeys)
		targetVersion := int64(r.Intn(int(maxVersion)) + 1)
		key := keys[keyIdx]

		// Use iterator: for each version from target down, SeekGE and check
		iter, _ := db.NewIter(nil)

		for v := targetVersion; v >= 1; v-- {
			seekKey := encodeVersionPrefix(v, key)
			if iter.SeekGE(seekKey) {
				// Check if we found exact match
				foundKey := iter.Key()
				if len(foundKey) >= 8+len(key) {
					foundV, foundUserKey := decodeVersionPrefix(foundKey)
					if foundV == v && string(foundUserKey) == string(key) {
						_ = iter.Value()
						break
					}
				}
			}
		}
		iter.Close()
	}
}

// =============================================================================
// COMPREHENSIVE BENCHMARKS: Version-Prefix + Index
// =============================================================================

// BenchmarkIndexedPrefixWrite benchmarks write with index maintenance
func BenchmarkIndexedPrefixWrite(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-indexed-write-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	keys := make([][]byte, 10000)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100)
	rand.Read(value)

	index := NewVersionIndex()

	b.ResetTimer()
	b.ReportAllocs()

	version := int64(1)
	for i := 0; i < b.N; i++ {
		batch := db.NewBatch()
		// Write 1000 keys per batch
		for j := 0; j < 1000; j++ {
			key := keys[j%len(keys)]
			encodedKey := encodeVersionPrefix(version, key)
			batch.Set(encodedKey, value, nil)
			index.Add(key, version) // Update index
		}
		batch.Commit(pebble.NoSync)
		batch.Close()
		version++
	}

	b.StopTimer()
	b.ReportMetric(float64(1000), "keys/op")
}

// BenchmarkIndexedPrefixRead benchmarks MVCC read with index
func BenchmarkIndexedPrefixRead(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-indexed-read-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	numKeys := 10000
	maxVersion := int64(100)
	keys := make([][]byte, numKeys)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100)
	rand.Read(value)

	index := NewVersionIndex()

	// Pre-populate with sparse data
	r := rand.New(rand.NewSource(42))
	for _, key := range keys {
		for j := 0; j < 10; j++ {
			v := int64(r.Intn(int(maxVersion)) + 1)
			encodedKey := encodeVersionPrefix(v, key)
			db.Set(encodedKey, value, pebble.NoSync)
			index.Add(key, v)
		}
	}
	db.Flush()

	b.ResetTimer()
	b.ReportAllocs()

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < b.N; i++ {
		keyIdx := r.Intn(numKeys)
		targetVersion := int64(r.Intn(int(maxVersion)) + 1)
		key := keys[keyIdx]

		// Index lookup + single Get
		bestVersion := index.FindLatestLE(key, targetVersion)
		if bestVersion > 0 {
			encodedKey := encodeVersionPrefix(bestVersion, key)
			val, closer, err := db.Get(encodedKey)
			if err == nil {
				_ = val
				closer.Close()
			}
		}
	}
}

// BenchmarkPrefixMixedWorkload benchmarks mixed read/write workload
func BenchmarkPrefixMixedWorkload(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-prefix-mixed-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openPrefixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Pre-populate
	numKeys := 10000
	keys := make([][]byte, numKeys)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
	}
	value := make([]byte, 100)
	rand.Read(value)

	// Initial population at version 1
	batch := db.NewBatch()
	for _, key := range keys {
		encodedKey := encodeVersionPrefix(1, key)
		if err := batch.Set(encodedKey, value, nil); err != nil {
			b.Fatal(err)
		}
	}
	batch.Commit(pebble.NoSync)
	batch.Close()

	b.ResetTimer()
	b.ReportAllocs()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	version := int64(2)

	for i := 0; i < b.N; i++ {
		// 80% reads, 20% writes
		if r.Float32() < 0.8 {
			// Read
			keyIdx := r.Intn(numKeys)
			readVersion := int64(r.Intn(int(version)) + 1)
			encodedKey := encodeVersionPrefix(readVersion, keys[keyIdx])
			val, closer, err := db.Get(encodedKey)
			if err == nil {
				_ = val
				closer.Close()
			}
		} else {
			// Write batch of 100 keys
			batch := db.NewBatch()
			for j := 0; j < 100; j++ {
				keyIdx := r.Intn(numKeys)
				encodedKey := encodeVersionPrefix(version, keys[keyIdx])
				batch.Set(encodedKey, value, nil)
			}
			batch.Commit(pebble.NoSync)
			batch.Close()
			version++
		}
	}
}

// TestPrefixKeyEncoding verifies the encoding is correct
func TestPrefixKeyEncoding(t *testing.T) {
	testCases := []struct {
		version int64
		key     string
	}{
		{1, "key-00000001"},
		{100, "account/balance"},
		{999999, "evm/storage/0x1234"},
	}

	for _, tc := range testCases {
		encoded := encodeVersionPrefix(tc.version, []byte(tc.key))
		decodedVersion, decodedKey := decodeVersionPrefix(encoded)

		if decodedVersion != tc.version {
			t.Errorf("version mismatch: got %d, want %d", decodedVersion, tc.version)
		}
		if string(decodedKey) != tc.key {
			t.Errorf("key mismatch: got %s, want %s", decodedKey, tc.key)
		}
	}
}

// TestPrefixKeyOrdering verifies lexicographic ordering groups by version first
func TestPrefixKeyOrdering(t *testing.T) {
	// With version prefix, keys are ordered by version first, then by key
	k1 := encodeVersionPrefix(1, []byte("zzz"))
	k2 := encodeVersionPrefix(2, []byte("aaa"))
	k3 := encodeVersionPrefix(2, []byte("bbb"))

	// k1 (v1) should come before k2 (v2) even though "zzz" > "aaa"
	if string(k1) >= string(k2) {
		t.Error("version 1 key should sort before version 2 key")
	}

	// k2 and k3 (same version) should sort by key
	if string(k2) >= string(k3) {
		t.Error("same version keys should sort by key content")
	}

	t.Logf("Key ordering verified:")
	t.Logf("  v1/zzz: %x", k1)
	t.Logf("  v2/aaa: %x", k2)
	t.Logf("  v2/bbb: %x", k3)
}
