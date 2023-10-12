package dbbackend

import (
	"fmt"
	"io/ioutil"
	"path"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/linxGnu/grocksdb"
	"github.com/sei-protocol/sei-db/benchmark/utils"
)

func writeToRocksDBConcurrently(db *grocksdb.DB, inputKVDir string, concurrency int, maxRetries int, chunkSize int) []time.Duration {
	// Create buffered channel to collect latencies with num kv entries
	files, err := ioutil.ReadDir(inputKVDir)
	if err != nil {
		panic(err)
	}
	latencies := make(chan time.Duration, len(files)*chunkSize)

	wg := &sync.WaitGroup{}
	processedFiles := &sync.Map{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		// Each goroutine will randomly select some available file, read its kv data and write to db
		go func() {
			defer wg.Done()
			wo := grocksdb.NewDefaultWriteOptions()
			for {
				filename := utils.PickRandomKVFile(inputKVDir, processedFiles)
				if filename == "" {
					break
				}
				kvEntries, _ := utils.ReadKVEntriesFromFile(path.Join(inputKVDir, filename))
				utils.RandomShuffle(kvEntries)

				for _, kv := range kvEntries {
					retries := 0
					for {
						startTime := time.Now()
						err := db.Put(wo, kv.Key, kv.Value)
						latency := time.Since(startTime)
						if err == nil {
							latencies <- latency
							break
						}
						retries++
						if retries > maxRetries {
							break
						}
					}
				}
			}
		}()
	}
	wg.Wait()
	close(latencies)

	var latencySlice []time.Duration
	for l := range latencies {
		latencySlice = append(latencySlice, l)
	}
	return latencySlice
}

func (rocksDB RocksDBBackend) BenchmarkDBWrite(inputKVDir string, outputDBPath string, concurrency int, maxRetries int, chunkSize int) {
	opts := grocksdb.NewDefaultOptions()
	// Configs taken from implementations
	opts.IncreaseParallelism(runtime.NumCPU())
	opts.OptimizeLevelStyleCompaction(512 * 1024 * 1024)
	opts.SetTargetFileSizeMultiplier(2)
	opts.SetOptimizeFiltersForHits(true)
	opts.SetCreateIfMissing(true)

	db, err := grocksdb.OpenDb(opts, outputDBPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to open the DB: %v", err))
	}
	defer db.Close()

	// Write shuffled entries to RocksDB concurrently
	startTime := time.Now()
	latencies := writeToRocksDBConcurrently(db, inputKVDir, concurrency, maxRetries, chunkSize)
	endTime := time.Now()

	totalTime := endTime.Sub(startTime)

	// Log throughput
	fmt.Printf("Total Successfully Written %d\n", len(latencies))
	fmt.Printf("Total Time taken: %v\n", totalTime)
	fmt.Printf("Throughput: %f writes/sec\n", float64(len(latencies))/totalTime.Seconds())
	fmt.Printf("Total records written %d\n", len(latencies))

	// Sort latencies for percentile calculations
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	// Calculate average latency
	var totalLatency time.Duration
	for _, l := range latencies {
		totalLatency += l
	}
	avgLatency := totalLatency / time.Duration(len(latencies))

	fmt.Printf("Average Latency: %v\n", avgLatency)
	fmt.Printf("P50 Latency: %v\n", utils.CalculatePercentile(latencies, 50))
	fmt.Printf("P75 Latency: %v\n", utils.CalculatePercentile(latencies, 75))
	fmt.Printf("P99 Latency: %v\n", utils.CalculatePercentile(latencies, 99))

}

func readFromRocksDBConcurrently(db *grocksdb.DB, inputKVDir string, concurrency int, maxRetries int, chunkSize int) []time.Duration {
	// Create buffered channel to collect latencies with num kv entries
	files, err := ioutil.ReadDir(inputKVDir)
	if err != nil {
		panic(err)
	}
	latencies := make(chan time.Duration, len(files)*chunkSize)

	processedFiles := &sync.Map{}
	wg := &sync.WaitGroup{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		// Each goroutine will randomly select some available file, read its kv data and read that key from db
		go func() {
			defer wg.Done()
			ro := grocksdb.NewDefaultReadOptions()
			for {
				filename := utils.PickRandomKVFile(inputKVDir, processedFiles)
				if filename == "" {
					break
				}
				kvEntries, _ := utils.ReadKVEntriesFromFile(path.Join(inputKVDir, filename))
				utils.RandomShuffle(kvEntries)

				for _, kv := range kvEntries {
					retries := 0
					for {
						startTime := time.Now()
						_, err := db.Get(ro, kv.Key)
						latency := time.Since(startTime)
						if err == nil {
							latencies <- latency
							break
						}
						retries++
						if retries > maxRetries {
							break
						}
					}
				}
			}
		}()
	}
	wg.Wait()
	close(latencies)

	var latencySlice []time.Duration
	for l := range latencies {
		latencySlice = append(latencySlice, l)
	}
	return latencySlice
}

func (rocksDB RocksDBBackend) BenchmarkDBRead(inputKVDir string, outputDBPath string, concurrency int, maxRetries int, chunkSize int) {
	// Open the DB with default options
	opts := grocksdb.NewDefaultOptions()
	opts.IncreaseParallelism(runtime.NumCPU())
	opts.OptimizeLevelStyleCompaction(512 * 1024 * 1024)
	opts.SetTargetFileSizeMultiplier(2)
	opts.SetOptimizeFiltersForHits(true)

	db, err := grocksdb.OpenDb(opts, outputDBPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to open the DB: %v", err))
	}
	defer db.Close()

	startTime := time.Now()
	latencies := readFromRocksDBConcurrently(db, inputKVDir, concurrency, maxRetries, chunkSize)
	endTime := time.Now()

	totalTime := endTime.Sub(startTime)

	// Log throughput
	fmt.Printf("Total Successfully Read %d\n", len(latencies))
	fmt.Printf("Total Time taken: %v\n", totalTime)
	fmt.Printf("Throughput: %f reads/sec\n", float64(len(latencies))/totalTime.Seconds())
	fmt.Printf("Total records read %d\n", len(latencies))

	// Sort latencies for percentile calculations
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	// Calculate average latency
	var totalLatency time.Duration
	for _, l := range latencies {
		totalLatency += l
	}
	avgLatency := totalLatency / time.Duration(len(latencies))

	fmt.Printf("Average Latency: %v\n", avgLatency)
	fmt.Printf("P50 Latency: %v\n", utils.CalculatePercentile(latencies, 50))
	fmt.Printf("P75 Latency: %v\n", utils.CalculatePercentile(latencies, 75))
	fmt.Printf("P99 Latency: %v\n", utils.CalculatePercentile(latencies, 99))
}
