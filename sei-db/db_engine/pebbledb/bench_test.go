package pebbledb

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
)

var benchRng = rand.New(rand.NewSource(567320))

// BenchmarkRawWrite benchmarks raw PebbleDB write throughput.
// Run with: go test -bench=BenchmarkRawWrite -benchtime=10s ./sei-db/db_engine/pebbledb/
func BenchmarkRawWrite(b *testing.B) {
	dir := b.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Pre-generate keys and values
	keys := make([][]byte, b.N)
	vals := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		key := make([]byte, 32)
		val := make([]byte, 256)
		benchRng.Read(key)
		benchRng.Read(val)
		keys[i] = key
		vals[i] = val
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := db.Set(keys[i], vals[i], db_engine.WriteOptions{Sync: false}); err != nil {
			b.Fatalf("Set: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "writes/sec")
}

// BenchmarkRawWriteSync benchmarks raw PebbleDB write throughput with sync.
func BenchmarkRawWriteSync(b *testing.B) {
	dir := b.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Pre-generate keys and values
	keys := make([][]byte, b.N)
	vals := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		key := make([]byte, 32)
		val := make([]byte, 256)
		benchRng.Read(key)
		benchRng.Read(val)
		keys[i] = key
		vals[i] = val
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := db.Set(keys[i], vals[i], db_engine.WriteOptions{Sync: true}); err != nil {
			b.Fatalf("Set: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "writes/sec")
}

// BenchmarkRawBatchWrite benchmarks raw PebbleDB batch write throughput.
func BenchmarkRawBatchWrite(b *testing.B) {
	const batchSize = 1000

	dir := b.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	totalOps := b.N * batchSize
	keys := make([][]byte, totalOps)
	vals := make([][]byte, totalOps)
	for i := 0; i < totalOps; i++ {
		key := make([]byte, 32)
		val := make([]byte, 256)
		benchRng.Read(key)
		benchRng.Read(val)
		keys[i] = key
		vals[i] = val
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		batch := db.NewBatch()
		for j := 0; j < batchSize; j++ {
			idx := i*batchSize + j
			if err := batch.Set(keys[idx], vals[idx]); err != nil {
				b.Fatalf("batch Set: %v", err)
			}
		}
		if err := batch.Commit(db_engine.WriteOptions{Sync: false}); err != nil {
			b.Fatalf("batch Commit: %v", err)
		}
		_ = batch.Close()
	}

	b.StopTimer()
	b.ReportMetric(float64(totalOps)/b.Elapsed().Seconds(), "writes/sec")
}

// BenchmarkRawRead benchmarks raw PebbleDB read throughput on pre-populated data.
func BenchmarkRawRead(b *testing.B) {
	const numKeys = 100000

	dir := b.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Populate with data
	keys := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		key := make([]byte, 32)
		val := make([]byte, 256)
		benchRng.Read(key)
		benchRng.Read(val)
		keys[i] = key
		if err := db.Set(key, val, db_engine.WriteOptions{Sync: false}); err != nil {
			b.Fatalf("Set: %v", err)
		}
	}

	// Flush to ensure data is persisted
	if err := db.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := keys[i%numKeys]
		_, err := db.Get(key)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "reads/sec")
}

// BenchmarkRawIterate benchmarks raw PebbleDB iteration throughput.
func BenchmarkRawIterate(b *testing.B) {
	const numKeys = 100000

	dir := b.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Populate with data
	for i := 0; i < numKeys; i++ {
		key := make([]byte, 32)
		val := make([]byte, 256)
		benchRng.Read(key)
		benchRng.Read(val)
		if err := db.Set(key, val, db_engine.WriteOptions{Sync: false}); err != nil {
			b.Fatalf("Set: %v", err)
		}
	}

	if err := db.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		itr, err := db.NewIter(nil)
		if err != nil {
			b.Fatalf("NewIter: %v", err)
		}

		count := 0
		for ok := itr.First(); ok && itr.Valid(); ok = itr.Next() {
			_ = itr.Key()
			_ = itr.Value()
			count++
		}
		_ = itr.Close()
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N*numKeys)/b.Elapsed().Seconds(), "keys/sec")
}

// RunRawBenchmarks runs all raw PebbleDB benchmarks and prints detailed results.
// This function can be called from a test for easy execution.
func RunRawBenchmarks(keySize, valueSize, numOps int) {
	fmt.Printf("=== PebbleDB Raw Benchmark ===\n")
	fmt.Printf("Key size: %d bytes, Value size: %d bytes, Operations: %d\n\n", keySize, valueSize, numOps)

	dir, err := createTempDir()
	if err != nil {
		fmt.Printf("Error creating temp dir: %v\n", err)
		return
	}

	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		fmt.Printf("Open error: %v\n", err)
		return
	}
	defer func() { _ = db.Close() }()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate test data
	keys := make([][]byte, numOps)
	vals := make([][]byte, numOps)
	for i := 0; i < numOps; i++ {
		key := make([]byte, keySize)
		val := make([]byte, valueSize)
		rng.Read(key)
		rng.Read(val)
		keys[i] = key
		vals[i] = val
	}

	// === Write Benchmark ===
	fmt.Println("--- Write Benchmark (NoSync) ---")
	start := time.Now()
	for i := 0; i < numOps; i++ {
		if err := db.Set(keys[i], vals[i], db_engine.WriteOptions{Sync: false}); err != nil {
			fmt.Printf("Set error: %v\n", err)
			return
		}
	}
	writeTime := time.Since(start)
	fmt.Printf("Total time: %v\n", writeTime)
	fmt.Printf("Throughput: %.2f writes/sec\n", float64(numOps)/writeTime.Seconds())
	fmt.Printf("Avg latency: %v\n\n", writeTime/time.Duration(numOps))

	// Flush
	if err := db.Flush(); err != nil {
		fmt.Printf("Flush error: %v\n", err)
		return
	}

	// === Read Benchmark ===
	fmt.Println("--- Read Benchmark ---")
	start = time.Now()
	for i := 0; i < numOps; i++ {
		_, err := db.Get(keys[i])
		if err != nil {
			fmt.Printf("Get error: %v\n", err)
			return
		}
	}
	readTime := time.Since(start)
	fmt.Printf("Total time: %v\n", readTime)
	fmt.Printf("Throughput: %.2f reads/sec\n", float64(numOps)/readTime.Seconds())
	fmt.Printf("Avg latency: %v\n\n", readTime/time.Duration(numOps))

	// === Batch Write Benchmark ===
	fmt.Println("--- Batch Write Benchmark (1000 ops/batch) ---")
	batchSize := 1000
	numBatches := numOps / batchSize

	// Generate new data for batch test
	batchKeys := make([][]byte, numOps)
	batchVals := make([][]byte, numOps)
	for i := 0; i < numOps; i++ {
		key := make([]byte, keySize)
		val := make([]byte, valueSize)
		rng.Read(key)
		rng.Read(val)
		batchKeys[i] = key
		batchVals[i] = val
	}

	start = time.Now()
	for i := 0; i < numBatches; i++ {
		batch := db.NewBatch()
		for j := 0; j < batchSize; j++ {
			idx := i*batchSize + j
			if err := batch.Set(batchKeys[idx], batchVals[idx]); err != nil {
				fmt.Printf("batch Set error: %v\n", err)
				return
			}
		}
		if err := batch.Commit(db_engine.WriteOptions{Sync: false}); err != nil {
			fmt.Printf("batch Commit error: %v\n", err)
			return
		}
		_ = batch.Close()
	}
	batchTime := time.Since(start)
	fmt.Printf("Total time: %v\n", batchTime)
	fmt.Printf("Throughput: %.2f writes/sec\n", float64(numBatches*batchSize)/batchTime.Seconds())
	fmt.Printf("Avg batch latency: %v\n\n", batchTime/time.Duration(numBatches))
}

// RunConcurrentBenchmarks runs concurrent read/write benchmarks.
func RunConcurrentBenchmarks(keySize, valueSize, numOps, concurrency int) {
	fmt.Printf("=== PebbleDB Concurrent Benchmark ===\n")
	fmt.Printf("Key size: %d bytes, Value size: %d bytes, Operations: %d, Concurrency: %d\n\n",
		keySize, valueSize, numOps, concurrency)

	dir, err := createTempDir()
	if err != nil {
		fmt.Printf("Error creating temp dir: %v\n", err)
		return
	}

	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		fmt.Printf("Open error: %v\n", err)
		return
	}
	defer func() { _ = db.Close() }()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate test data
	keys := make([][]byte, numOps)
	vals := make([][]byte, numOps)
	for i := 0; i < numOps; i++ {
		key := make([]byte, keySize)
		val := make([]byte, valueSize)
		rng.Read(key)
		rng.Read(val)
		keys[i] = key
		vals[i] = val
	}

	// === Concurrent Write Benchmark ===
	fmt.Println("--- Concurrent Write Benchmark ---")
	opsPerWorker := numOps / concurrency

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			startIdx := workerID * opsPerWorker
			endIdx := startIdx + opsPerWorker
			for i := startIdx; i < endIdx; i++ {
				if err := db.Set(keys[i], vals[i], db_engine.WriteOptions{Sync: false}); err != nil {
					fmt.Printf("Worker %d Set error: %v\n", workerID, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	writeTime := time.Since(start)
	fmt.Printf("Total time: %v\n", writeTime)
	fmt.Printf("Throughput: %.2f writes/sec\n", float64(numOps)/writeTime.Seconds())
	fmt.Printf("Avg latency: %v\n\n", writeTime/time.Duration(numOps))

	// Flush
	if err := db.Flush(); err != nil {
		fmt.Printf("Flush error: %v\n", err)
		return
	}

	// === Concurrent Read Benchmark ===
	fmt.Println("--- Concurrent Read Benchmark ---")
	start = time.Now()
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			startIdx := workerID * opsPerWorker
			endIdx := startIdx + opsPerWorker
			for i := startIdx; i < endIdx; i++ {
				_, err := db.Get(keys[i])
				if err != nil {
					fmt.Printf("Worker %d Get error: %v\n", workerID, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	readTime := time.Since(start)
	fmt.Printf("Total time: %v\n", readTime)
	fmt.Printf("Throughput: %.2f reads/sec\n", float64(numOps)/readTime.Seconds())
	fmt.Printf("Avg latency: %v\n\n", readTime/time.Duration(numOps))
}

func createTempDir() (string, error) {
	return "/tmp/pebble-bench-" + fmt.Sprintf("%d", time.Now().UnixNano()), nil
}

// TestRunBenchmarks is a test that runs the benchmarks for easy execution.
// Run with: go test -v -run=TestRunBenchmarks ./sei-db/db_engine/pebbledb/ -timeout=10m
func TestRunBenchmarks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	// Run single-threaded benchmarks
	RunRawBenchmarks(32, 256, 100000)

	// Run concurrent benchmarks
	RunConcurrentBenchmarks(32, 256, 100000, 4)
}
