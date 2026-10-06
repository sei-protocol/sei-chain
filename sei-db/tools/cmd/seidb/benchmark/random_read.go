package benchmark

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func DBRandomReadCmd() *cobra.Command {
	benchmarkReadCmd := &cobra.Command{
		Use:   "benchmark-read",
		Short: "Benchmark read is designed to measure read performance of different db backends",
		Run:   executeRandomRead,
	}

	benchmarkReadCmd.PersistentFlags().StringP("db-backend", "d", "", "DB Backend")
	benchmarkReadCmd.PersistentFlags().StringP("raw-kv-input-dir", "r", "", "Input Directory for benchmark which contains the raw kv data")
	benchmarkReadCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	benchmarkReadCmd.PersistentFlags().IntP("concurrency", "c", 1, "Concurrency while writing to db")
	benchmarkReadCmd.PersistentFlags().Int64P("max-operations", "p", 1000, "Max operations to run")
	benchmarkReadCmd.PersistentFlags().IntP("num-versions", "v", 1, "number of versions in db")

	return benchmarkReadCmd
}

func executeRandomRead(cmd *cobra.Command, args []string) {
	dbBackend, _ := cmd.Flags().GetString("db-backend")
	rawKVInputDir, _ := cmd.Flags().GetString("raw-kv-input-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	numVersions, _ := cmd.Flags().GetInt("num-versions")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	maxOps, _ := cmd.Flags().GetInt64("max-operations")

	if dbBackend == "" {
		panic("Must provide db backend when benchmarking")
	}

	if rawKVInputDir == "" {
		panic("Must provide raw kv input dir when benchmarking")
	}

	if outputDir == "" {
		panic("Must provide output dir")
	}

	_, isAcceptedBackend := ValidDBBackends[dbBackend]
	if !isAcceptedBackend {
		panic(fmt.Sprintf("Unsupported db backend: %s\n", dbBackend))
	}

	DBRandomRead(rawKVInputDir, numVersions, outputDir, dbBackend, concurrency, maxOps)
}

// BenchmarkRead read latencies and throughput of db backend
func DBRandomRead(inputKVDir string, numVersions int, outputDir string, dbBackend string, concurrency int, maxOps int64) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}
	// Iterate over files in directory
	fmt.Printf("Reading Raw Keys and Values from %s\n", inputKVDir)
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = dbBackend
	backend, err := ss.NewStateStore(outputDir, ssConfig)
	if err != nil {
		panic(err)
	}
	benchmarkDBRead(backend, inputKVDir, numVersions, concurrency, maxOps)
	_ = backend.Close()
}

// benchmarkDBRead measures random read performance of the db
// Given an input dir containing all the raw kv data, it generates random read load and measures performance.
func benchmarkDBRead(db types.StateStore, inputKVDir string, numVersions int, concurrency int, maxOps int64) {
	kvData, err := utils.LoadAndShuffleKV(inputKVDir, concurrency)
	if err != nil {
		panic(err)
	}
	startTime := time.Now()
	latencies := readFromDBConcurrently(db, kvData, numVersions, concurrency, maxOps)
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

// readFromDBConcurrently generates random read load against the db
// Given kv pairs (randomly shuffled), numVersions, it will spin up `concurrency` goroutines
// that randomly select a version, key and query the db.
// It only performs `maxOps“ random reads and maintains a `latencies` channel which aggregates all the latencies.
func readFromDBConcurrently(db types.StateStore, allKVs []utils.KeyValuePair, numVersions int, concurrency int, maxOps int64) []time.Duration {
	allLatencies := make([]time.Duration, 0, maxOps)
	latencies := make(chan time.Duration, maxOps)

	var opCounter int64
	wg := &sync.WaitGroup{}

	// Each goroutine will handle reading a subset of kv pairs
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				currentOps := atomic.AddInt64(&opCounter, 1)
				if currentOps > maxOps {
					break
				}
				// Randomly pick a version and retrieve its column family handle
				version := int64(rand.Intn(numVersions)) + 1

				// Randomly pick a key-value pair to read
				kv := allKVs[rand.Intn(len(allKVs))]

				startTime := time.Now()
				// No store key for benchmarks
				_, err := db.Get("", version, kv.Key)
				latency := time.Since(startTime)
				if err == nil {
					latencies <- latency
				} else {
					panic(err)
				}

			}
		}()
	}
	wg.Wait()
	close(latencies)

	for l := range latencies {
		allLatencies = append(allLatencies, l)
	}

	return allLatencies
}
