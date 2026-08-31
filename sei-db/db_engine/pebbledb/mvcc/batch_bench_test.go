package mvcc

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
)

// BenchmarkPebbleBatchFill compares filling a Pebble batch. grow vs preSize
// use already-encoded slices and isolate buffer growth. encodeThenSet is the
// old write path (heap-encode then Set copy). deferred encodes into
// SetDeferred slots. Commit is excluded so memtable insert does not hide
// buffer growth.
func BenchmarkPebbleBatchFill(b *testing.B) {
	for _, n := range []int{10_000, 500_000} {
		raw := makeBenchBatchOps(n)
		encodedKeys := make([][]byte, n)
		encodedVals := make([][]byte, n)
		for i, op := range raw {
			encodedKeys[i] = encodeMVCCStoreKey(op.storeKey, op.key, op.version, true)
			encodedVals[i] = MVCCEncode(op.value, op.tombstone, true)
		}
		size := pebbleBatchBufSize(raw, false)
		payload := int64(size)

		b.Run(fmt.Sprintf("n=%d/grow", n), func(b *testing.B) {
			db := openBenchPebble(b)
			b.SetBytes(payload)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				batch := db.NewBatch(pebble.WithMaxRetainedSizeBytes(1))
				fillPebbleBatchSet(b, batch, encodedKeys, encodedVals)
				if err := batch.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("n=%d/preSize", n), func(b *testing.B) {
			db := openBenchPebble(b)
			b.SetBytes(payload)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				batch := db.NewBatchWithSize(size, pebble.WithMaxRetainedSizeBytes(1))
				fillPebbleBatchSet(b, batch, encodedKeys, encodedVals)
				if err := batch.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("n=%d/encodeThenSet", n), func(b *testing.B) {
			db := openBenchPebble(b)
			b.SetBytes(payload)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				batch := db.NewBatchWithSize(size, pebble.WithMaxRetainedSizeBytes(1))
				fillPebbleBatchEncodeThenSet(b, batch, raw)
				if err := batch.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("n=%d/deferred", n), func(b *testing.B) {
			db := openBenchPebble(b)
			b.SetBytes(payload)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				batch := db.NewBatchWithSize(size, pebble.WithMaxRetainedSizeBytes(1))
				if err := encodeOpsDeferred(batch, raw, true); err != nil {
					b.Fatal(err)
				}
				if err := batch.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func fillPebbleBatchSet(b *testing.B, batch *pebble.Batch, keys, vals [][]byte) {
	b.Helper()
	for i := range keys {
		if err := batch.Set(keys[i], vals[i], nil); err != nil {
			b.Fatal(err)
		}
	}
}

func fillPebbleBatchEncodeThenSet(b *testing.B, batch *pebble.Batch, ops []batchOp) {
	b.Helper()
	for _, op := range ops {
		key := encodeMVCCStoreKey(op.storeKey, op.key, op.version, true)
		val := MVCCEncode(op.value, op.tombstone, true)
		if err := batch.Set(key, val, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func makeBenchBatchOps(n int) []batchOp {
	ops := make([]batchOp, n)
	var key [8]byte
	val := []byte("value")
	for i := 0; i < n; i++ {
		binary.BigEndian.PutUint64(key[:], uint64(i))
		k := make([]byte, 8)
		copy(k, key[:])
		v := make([]byte, len(val))
		copy(v, val)
		ops[i] = batchOp{
			storeKey: "s",
			key:      k,
			value:    v,
			version:  1,
		}
	}
	return ops
}

func openBenchPebble(b *testing.B) *pebble.DB {
	b.Helper()
	db, err := pebble.Open("bench", &pebble.Options{
		FS:     vfs.NewMem(),
		Logger: silentPebbleLogger{},
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	})
	return db
}

type silentPebbleLogger struct{}

func (silentPebbleLogger) Infof(string, ...interface{})  {}
func (silentPebbleLogger) Errorf(string, ...interface{}) {}
func (silentPebbleLogger) Fatalf(format string, args ...interface{}) {
	panic("pebble: " + fmt.Sprintf(format, args...))
}

type rawKV struct{ k, v []byte }

// rawInput builds EVM-shaped input: 0x03 || 20-byte address || 32-byte slot
// keys with 32-byte values, the mix pebblesim generates.
func rawInput(n int, seed int64) []rawKV {
	rng := rand.New(rand.NewSource(seed))
	out := make([]rawKV, n)
	for i := range out {
		k := make([]byte, 53)
		k[0] = 3
		rng.Read(k[1:])
		v := make([]byte, 32)
		rng.Read(v)
		out[i] = rawKV{k, v}
	}
	return out
}

// BenchmarkApplyChangeset500k measures Set-all-then-Write at 500k keys, the
// shape of ApplyChangesetSync, with and without the caller presorting.
func BenchmarkApplyChangeset500k(b *testing.B) {
	const n = 500_000
	in := rawInput(n, 9)
	sorted := append([]rawKV(nil), in...)
	{
		ops := make([]batchOp, 0, n)
		for _, kv := range sorted {
			ops = append(ops, batchOp{storeKey: "evm", key: kv.k, value: kv.v, version: 1})
		}
		sortBatchOps(ops, true)
		for i, op := range ops {
			sorted[i] = rawKV{op.key, op.value}
		}
	}

	run := func(name string, src []rawKV) {
		b.Run(name, func(b *testing.B) {
			db := openBenchPebble(b)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				nb, err := NewBatch(db, 1, true, "bench")
				if err != nil {
					b.Fatal(err)
				}
				nb.grow(n)
				for _, kv := range src {
					if err := nb.Set("evm", kv.k, kv.v); err != nil {
						b.Fatal(err)
					}
				}
				if err := nb.Write(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	run("unsortedInput", in)
	run("presortedInput", sorted)
}

// BenchmarkSortBatchOps500k isolates the sort step alone.
func BenchmarkSortBatchOps500k(b *testing.B) {
	const n = 500_000
	in := rawInput(n, 2)
	src := make([]batchOp, n)
	for i, kv := range in {
		src[i] = batchOp{storeKey: "evm", key: kv.k, value: kv.v, version: 1}
	}
	pre := append([]batchOp(nil), src...)
	sortBatchOps(pre, true)

	dst := make([]batchOp, n)
	b.Run("unsorted", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			copy(dst, src)
			sortBatchOps(dst, true)
		}
	})
	b.Run("alreadySorted", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			copy(dst, pre)
			sortBatchOps(dst, true)
		}
	})
}
