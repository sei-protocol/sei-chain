package mvcc

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
)

// MVCC Suffix Key Encoding: <key>\x00[<version>]<#version-bytes>
// This is the current encoding used in sei-db

// openSuffixDB opens a PebbleDB with MVCCComparer for version-suffixed keys
func openSuffixDB(dir string) (*pebble.DB, error) {
	cache := pebble.NewCache(1024 * 1024 * 32) // 32MB cache
	defer cache.Unref()

	opts := &pebble.Options{
		Cache:                       cache,
		Comparer:                    MVCCComparer, // MVCC comparer for suffix encoding
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

// BenchmarkSuffixWrite benchmarks write throughput with MVCC suffix keys
func BenchmarkSuffixWrite(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-suffix-write-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openSuffixDB(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Pre-generate keys (no store key prefix, just raw keys)
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
			encodedKey := MVCCEncode(key, version)
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

// BenchmarkSuffixRead benchmarks MVCC read with suffix encoding
func BenchmarkSuffixRead(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-suffix-read-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openSuffixDB(dir)
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

	// Pre-populate with sparse data (same as prefix benchmark)
	r := rand.New(rand.NewSource(42))
	for _, key := range keys {
		for j := 0; j < 10; j++ {
			v := int64(r.Intn(int(maxVersion)) + 1)
			encodedKey := MVCCEncode(key, v)
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

		// MVCC suffix: SeekGE to key@targetVersion, check if <= target
		seekKey := MVCCEncode(key, targetVersion)
		iter, _ := db.NewIter(&pebble.IterOptions{})

		found := false
		iter.SeekGE(seekKey)
		if iter.Valid() {
			foundKey, versionBytes, ok := SplitMVCCKey(iter.Key())
			if ok && string(foundKey) == string(key) {
				if len(versionBytes) > 0 {
					v, _ := decodeUint64Ascending(versionBytes)
					if v <= targetVersion {
						_ = iter.Value()
						found = true
					}
				}
			}
		}
		// Try previous key if not exact match
		if !found && iter.Valid() {
			iter.Prev()
			if iter.Valid() {
				foundKey, versionBytes, ok := SplitMVCCKey(iter.Key())
				if ok && string(foundKey) == string(key) && len(versionBytes) > 0 {
					v, _ := decodeUint64Ascending(versionBytes)
					if v <= targetVersion {
						_ = iter.Value()
					}
				}
			}
		}
		iter.Close()
	}
}

// BenchmarkSuffixMixedWorkload benchmarks mixed read/write workload
func BenchmarkSuffixMixedWorkload(b *testing.B) {
	dir, err := os.MkdirTemp("", "pebble-suffix-mixed-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := openSuffixDB(dir)
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
		encodedKey := MVCCEncode(key, 1)
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
			encodedKey := MVCCEncode(keys[keyIdx], readVersion)
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
				encodedKey := MVCCEncode(keys[keyIdx], version)
				batch.Set(encodedKey, value, nil)
			}
			batch.Commit(pebble.NoSync)
			batch.Close()
			version++
		}
	}
}
