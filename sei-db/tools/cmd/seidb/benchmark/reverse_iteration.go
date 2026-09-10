package benchmark

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func DBReverseIterationCmd() *cobra.Command {
	benchmarkReverseIterationCmd := &cobra.Command{
		Use:   "benchmark-reverse-iteration",
		Short: "Benchmark reverse iteration is designed to measure reverse iteration performance of different db backends",
		Run:   executeReverseIteration,
	}

	benchmarkReverseIterationCmd.PersistentFlags().StringP("db-backend", "d", "", "DB Backend")
	benchmarkReverseIterationCmd.PersistentFlags().StringP("raw-kv-input-dir", "r", "", "Input Directory for benchmark which contains the raw kv data")
	benchmarkReverseIterationCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	benchmarkReverseIterationCmd.PersistentFlags().IntP("concurrency", "c", 1, "Concurrency while writing to db")
	benchmarkReverseIterationCmd.PersistentFlags().Int64P("max-operations", "p", 1000, "Max operations to run")
	benchmarkReverseIterationCmd.PersistentFlags().IntP("num-versions", "v", 1, "number of versions in db")
	benchmarkReverseIterationCmd.PersistentFlags().IntP("iteration-steps", "i", 10, "Number of steps to run per iteration")

	return benchmarkReverseIterationCmd
}

func executeReverseIteration(cmd *cobra.Command, args []string) {
	dbBackend, _ := cmd.Flags().GetString("db-backend")
	rawKVInputDir, _ := cmd.Flags().GetString("raw-kv-input-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	numVersions, _ := cmd.Flags().GetInt("num-versions")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	maxOps, _ := cmd.Flags().GetInt64("max-operations")
	iterationSteps, _ := cmd.Flags().GetInt("iteration-steps")

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

	DBReverseIteration(rawKVInputDir, numVersions, outputDir, dbBackend, concurrency, maxOps, iterationSteps)
}

// BenchmarkDBReverseIteration reverse iteration performance of db backend
func DBReverseIteration(inputKVDir string, numVersions int, outputDir string, dbBackend string, concurrency int, maxOps int64, iterationSteps int) {
	// Reverse Iterate over db at directory
	fmt.Printf("Iterating Over DB at  %s\n", outputDir)
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = dbBackend
	backend, err := ss.NewStateStore(outputDir, ssConfig)
	if err != nil {
		panic(err)
	}
	benchmarkDBReverseIteration(backend, inputKVDir, numVersions, concurrency, maxOps, iterationSteps)
	_ = backend.Close()
}

// benchmarkDBReverseIteration measures reverse iteration performance of the db
// Given an input dir containing all the raw kv data, it selects a random key, reverse iterates and measures performance.
func benchmarkDBReverseIteration(db types.StateStore, inputKVDir string, numVersions int, concurrency int, maxOps int64, iterationSteps int) {
	kvData, err := utils.LoadAndShuffleKV(inputKVDir, concurrency)
	if err != nil {
		panic(err)
	}

	startTime := time.Now()
	latencies, totalCountIteration := reverseIterateDBConcurrently(db, kvData, numVersions, concurrency, iterationSteps, maxOps)
	endTime := time.Now()

	totalTime := endTime.Sub(startTime)

	// Log throughput
	fmt.Printf("Total Prefixes Reverse-Iterated: %d\n", totalCountIteration)
	fmt.Printf("Total Time taken: %v\n", totalTime)
	fmt.Printf("Throughput: %f iterations/sec\n", float64(totalCountIteration)/totalTime.Seconds())

	// Calculate average latency
	var totalLatency time.Duration
	for _, l := range latencies {
		totalLatency += l
	}
	avgLatency := time.Duration(int64(totalLatency) / int64(totalCountIteration))
	fmt.Printf("Average Per-Key Latency: %v\n", avgLatency)
}

// reverseIterateDBConcurrently generates reverse iteration load against the db
// Given kv pairs (randomly shuffled), numVersions, it will spin up `concurrency` goroutines
// that randomly select a version, key, seeks to that key and starts a reverse iteration for at most `numIterationSteps` steps.
// It only performs `maxOps“ reverse iterations and maintains a `latencies` channel which aggregates all the latencies.
func reverseIterateDBConcurrently(db types.StateStore, allKVs []utils.KeyValuePair, numVersions int, concurrency int, numIterationSteps int, maxOps int64) ([]time.Duration, int) {
	allLatencies := make([]time.Duration, 0, maxOps)
	var totalSteps int
	latencies := make(chan time.Duration, maxOps)
	steps := make(chan int, maxOps)

	var opCounter int64
	wg := &sync.WaitGroup{}

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
				version := int64(rand.Intn(numVersions))

				// Randomly pick a key-value pair to seek to
				kv := allKVs[rand.Intn(len(allKVs))]

				startTime := time.Now()
				// No start key since we iterate for fixed numIterationSteps steps
				it, err := db.ReverseIterator("", version, nil, kv.Key)
				if err != nil {
					panic(err)
				}

				step := 0
				for step < numIterationSteps && it.Valid() {
					step++
					it.Next()
				}
				latency := time.Since(startTime)

				latencies <- latency
				steps <- step
				_ = it.Close()
			}
		}()
	}

	wg.Wait()
	close(latencies)
	close(steps)

	for l := range latencies {
		allLatencies = append(allLatencies, l)
	}

	for s := range steps {
		totalSteps += s
	}

	return allLatencies, totalSteps
}
