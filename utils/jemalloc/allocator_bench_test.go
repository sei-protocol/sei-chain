package jemalloc_test

import (
	"context"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/utils/jemalloc"
)

// The Pebble workload below is the C-heap hot path in seid: memtables and the
// block cache are manually allocated through cgo, and every sstable block is
// zstd-compressed and decompressed in C. The benchmark names are identical in
// both builds so benchstat lines them up as before/after columns:
//
//	go test -run '^$' -bench BenchmarkPebbleCHeap -count 10 ./utils/jemalloc/ > libc.txt
//	go test -run '^$' -bench BenchmarkPebbleCHeap -count 10 -tags jemalloc ./utils/jemalloc/ > jemalloc.txt
//	benchstat libc.txt jemalloc.txt
//
// SEI_ALLOC_BENCH_KEYS scales the working set (default 200_000 keys of 1 KiB,
// about 200 MiB before compression). If jemalloc lives outside the default
// search path, keep the optimisation flags when pointing cgo at it, e.g.
// CGO_CFLAGS="-g -O2 -I$PREFIX/include": overriding CGO_CFLAGS drops the
// default -O2 and slows every cgo dependency, not just this package.

const (
	benchValueSize = 1 << 10
	benchBatchSize = 1 << 10
)

func benchKeyCount(tb testing.TB) int {
	if v := os.Getenv("SEI_ALLOC_BENCH_KEYS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoError(tb, err, "SEI_ALLOC_BENCH_KEYS")
		return n
	}
	return 200_000
}

func benchKey(i int) []byte {
	k := make([]byte, 8)
	binary.BigEndian.PutUint64(k, uint64(i))
	return k
}

// randFill overwrites the first half of value with random bytes so zstd stays
// busy without compressing the payload to nothing.
func randFill(rng *rand.Rand, value []byte) {
	for i := 0; i+8 <= len(value)/2; i += 8 {
		binary.LittleEndian.PutUint64(value[i:], rng.Uint64())
	}
}

func openBenchDB(b *testing.B) types.KeyValueDB {
	cfg := pebbledb.DefaultConfig()
	cfg.DataDir = b.TempDir()
	cfg.EnableMetrics = false
	db, err := pebbledb.Open(context.Background(), &cfg)
	require.NoError(b, err)
	return db
}

func fillBenchDB(b *testing.B, db types.KeyValueDB, keys int, rng *rand.Rand) {
	value := make([]byte, benchValueSize)
	for start := 0; start < keys; start += benchBatchSize {
		batch := db.NewBatch()
		for i := start; i < start+benchBatchSize && i < keys; i++ {
			randFill(rng, value)
			require.NoError(b, batch.Set(benchKey(i), value))
		}
		require.NoError(b, batch.Commit(types.WriteOptions{Sync: false}))
		require.NoError(b, batch.Close())
	}
	require.NoError(b, db.Flush())
}

// residentBytes returns the process RSS from /proc, or 0 where /proc is absent.
func residentBytes() uint64 {
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(statm))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}

// reportAllocatorMetrics records the process footprint at the end of a
// sub-benchmark; the C heap dominates it because the Go heap here is tiny.
func reportAllocatorMetrics(b *testing.B) {
	if rss := residentBytes(); rss > 0 {
		b.ReportMetric(float64(rss)/(1<<20), "rss-MiB")
	}
	if jemalloc.Enabled {
		b.ReportMetric(float64(jemalloc.Allocated())/(1<<20), "jemalloc-MiB")
	}
}

// BenchmarkPebbleCHeap measures the cgo-allocating Pebble paths under
// whichever C allocator the test binary is linked against.
func BenchmarkPebbleCHeap(b *testing.B) {
	keys := benchKeyCount(b)
	b.Logf("allocator=%s keys=%d", allocatorName(), keys)

	b.Run("fill", func(b *testing.B) {
		rng := rand.New(rand.NewPCG(1, 2))
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			db := openBenchDB(b)
			b.StartTimer()
			fillBenchDB(b, db, keys, rng)
			b.StopTimer()
			require.NoError(b, db.Close())
			b.StartTimer()
		}
		reportAllocatorMetrics(b)
	})

	b.Run("randread", func(b *testing.B) {
		rng := rand.New(rand.NewPCG(3, 4))
		db := openBenchDB(b)
		defer func() { require.NoError(b, db.Close()) }()
		fillBenchDB(b, db, keys, rng)
		for b.Loop() {
			if _, err := db.Get(benchKey(rng.IntN(keys))); err != nil {
				b.Fatal(err)
			}
		}
		reportAllocatorMetrics(b)
	})

	b.Run("overwrite", func(b *testing.B) {
		rng := rand.New(rand.NewPCG(5, 6))
		db := openBenchDB(b)
		defer func() { require.NoError(b, db.Close()) }()
		fillBenchDB(b, db, keys, rng)
		value := make([]byte, benchValueSize)
		for b.Loop() {
			batch := db.NewBatch()
			for i := 0; i < benchBatchSize; i++ {
				randFill(rng, value)
				if err := batch.Set(benchKey(rng.IntN(keys)), value); err != nil {
					b.Fatal(err)
				}
			}
			if err := batch.Commit(types.WriteOptions{Sync: false}); err != nil {
				b.Fatal(err)
			}
			if err := batch.Close(); err != nil {
				b.Fatal(err)
			}
		}
		reportAllocatorMetrics(b)
	})
}

func allocatorName() string {
	if jemalloc.Enabled {
		return "jemalloc " + jemalloc.Version()
	}
	return "libc"
}
