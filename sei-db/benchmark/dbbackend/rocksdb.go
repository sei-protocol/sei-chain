package dbbackend

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/linxGnu/grocksdb"
	"github.com/sei-protocol/sei-db/benchmark/utils"
)

func writeToRocksDBConcurrently(db *grocksdb.DB, kvEntries []utils.KeyValuePair, concurrency int, maxRetries int) []time.Duration {
	// Channel to collect write latencies
	latencies := make(chan time.Duration, len(kvEntries))
	wg := &sync.WaitGroup{}
	chunks := len(kvEntries) / concurrency
	for i := 0; i < concurrency; i++ {
		start := i * chunks
		end := start + chunks
		if i == concurrency-1 {
			end = len(kvEntries)
		}
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			wo := grocksdb.NewDefaultWriteOptions()
			for j := start; j < end; j++ {
				retries := 0
				for {
					startTime := time.Now()
					err := db.Put(wo, kvEntries[j].Key, kvEntries[j].Value)
					latency := time.Since(startTime)

					// Only record latencies of successful writes
					if err == nil {
						latencies <- latency
					}

					if err != nil {
						retries++
						if retries > maxRetries {
							log.Printf("Failed to write key after %d attempts: %v", maxRetries, err)
							break
						}
						// TODO: Add a sleep or back-off before retrying
						// time.Sleep(time.Second * time.Duration(retries))
					} else {
						// Success, so break the retry loop
						break
					}
				}
			}
		}(start, end)
	}
	wg.Wait()
	close(latencies)

	var latencySlice []time.Duration
	for l := range latencies {
		latencySlice = append(latencySlice, l)
	}

	return latencySlice
}

func (rocksDB RocksDBBackend) BenchmarkDBWrite(inputKVDir string, outputDBPath string, concurrency int, maxRetries int) {
	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)

	db, err := grocksdb.OpenDb(opts, outputDBPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to open the DB: %v", err))
	}
	defer db.Close()

	// Read key-value entries from the file
	kvEntries, err := utils.ReadKVEntriesFromFile(inputKVDir)
	if err != nil {
		panic(fmt.Sprintf("Failed to read KV entries: %v", err))
	}

	// Shuffle the entries
	// NOTE: Adding in chunking so that it will shuffle across files
	utils.RandomShuffle(kvEntries)

	// Write shuffled entries to RocksDB concurrently
	startTime := time.Now()
	latencies := writeToRocksDBConcurrently(db, kvEntries, concurrency, maxRetries)
	endTime := time.Now()

	totalTime := endTime.Sub(startTime)

	// Log throughput
	log.Printf("Total Time taken: %v", totalTime)
	log.Printf("Throughput: %f ops/sec", float64(len(kvEntries))/totalTime.Seconds())

	// Sort latencies for percentile calculations
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	// Calculate average latency
	var totalLatency time.Duration
	for _, l := range latencies {
		totalLatency += l
	}
	avgLatency := totalLatency / time.Duration(len(latencies))

	log.Printf("Average Latency: %v", avgLatency)
	log.Printf("P50 Latency: %v", utils.CalculatePercentile(latencies, 50))
	log.Printf("P75 Latency: %v", utils.CalculatePercentile(latencies, 75))
	log.Printf("P99 Latency: %v", utils.CalculatePercentile(latencies, 99))

}

func (rocksDB RocksDBBackend) BenchmarkDBRead(inputKVDir string, outputDBPath string, concurrency int) {
	return
}
