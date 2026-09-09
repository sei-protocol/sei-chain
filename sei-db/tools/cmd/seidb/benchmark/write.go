package benchmark

import (
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func DBWriteCmd() *cobra.Command {
	benchmarkWriteCmd := &cobra.Command{
		Use:   "benchmark-write",
		Short: "Benchmark write is designed to measure write performance of different db backends",
		Run:   executeWrite,
	}

	benchmarkWriteCmd.PersistentFlags().StringP("db-backend", "d", "", "DB Backend")
	benchmarkWriteCmd.PersistentFlags().StringP("raw-kv-input-dir", "r", "", "Input Directory for benchmark which contains the raw kv data")
	benchmarkWriteCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	benchmarkWriteCmd.PersistentFlags().IntP("concurrency", "c", 1, "Concurrency while writing to db")
	benchmarkWriteCmd.PersistentFlags().IntP("batch-size", "b", 1, "batch size for db writes")
	benchmarkWriteCmd.PersistentFlags().IntP("num-versions", "v", 1, "number of versions in db")

	return benchmarkWriteCmd
}

func executeWrite(cmd *cobra.Command, args []string) {
	dbBackend, _ := cmd.Flags().GetString("db-backend")
	rawKVInputDir, _ := cmd.Flags().GetString("raw-kv-input-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	numVersions, _ := cmd.Flags().GetInt("num-versions")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	batchSize, _ := cmd.Flags().GetInt("batch-size")

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

	DBWrite(rawKVInputDir, numVersions, outputDir, dbBackend, concurrency, batchSize)
}

// BenchmarkWrite write latencies and throughput of db backend
func DBWrite(inputKVDir string, numVersions int, outputDir string, dbBackend string, concurrency int, batchSize int) {
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
	benchmarkDBWrite(backend, inputKVDir, numVersions, concurrency, batchSize)
	_ = backend.Close()
}

// benchmarkDBWrite measures random write performance of the db
// Given an input dir containing all the raw kv data, it writes to the db one version after another
func benchmarkDBWrite(db types.StateStore, inputKVDir string, numVersions int, concurrency int, batchSize int) {
	startLoad := time.Now()
	kvData, err := utils.LoadAndShuffleKV(inputKVDir, concurrency)
	if err != nil {
		panic(err)
	}
	endLoad := time.Now()
	fmt.Printf("Finishing loading %+v kv pairs into memory %+v\n", len(kvData), endLoad.Sub(startLoad).String())

	// Write each version sequentially
	totalTime := time.Duration(0)
	writeCount := 0
	for v := 1; v < numVersions+1; v++ {
		// Write shuffled entries to RocksDB concurrently
		fmt.Printf("On Version %+v\n", v)
		startTime := time.Now()
		latencies := writeToDBConcurrently(db, kvData, concurrency, int64(v), batchSize)
		endTime := time.Now()
		totalTime += endTime.Sub(startTime)
		writeCount += len(latencies)

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		// Latencies per version
		fmt.Printf("P50 Latency: %v\n", utils.CalculatePercentile(latencies, 50))
		fmt.Printf("P75 Latency: %v\n", utils.CalculatePercentile(latencies, 75))
		fmt.Printf("P99 Latency: %v\n", utils.CalculatePercentile(latencies, 99))
		fmt.Printf("Total time: %v\n", totalTime)
		fmt.Printf("Total Successfully Written %d\n", writeCount)
		runtime.GC()
	}

	// Log throughput
	fmt.Printf("Total Successfully Written %d\n", writeCount)
	fmt.Printf("Total Time taken: %v\n", totalTime)
	fmt.Printf("Throughput: %f writes/sec\n", float64(writeCount)/totalTime.Seconds())
	fmt.Printf("Total records written %d\n", writeCount)
}

// writeToDBConcurrently generates random write load against the db
// Given kv pairs (randomly shuffled), the version, batch size, it will spin up `concurrency` goroutines
// each of which is assigned to a portion of the kv data and writes to db in `batchSize` batches.
// It maintains a `latencies` channel which aggregates all the latencies
func writeToDBConcurrently(db types.StateStore, allKVs []utils.KeyValuePair, concurrency int, version int64, batchSize int) []time.Duration {
	allKVsLen := len(allKVs)
	allLatencies := make([]time.Duration, 0, allKVsLen)
	latencies := make(chan time.Duration, allKVsLen)

	kvsPerRoutine := allKVsLen / concurrency
	remainder := allKVsLen % concurrency

	wg := &sync.WaitGroup{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := i * kvsPerRoutine
			end := start + kvsPerRoutine

			if i == concurrency-1 {
				end += remainder
			}

			for j := start; j < end; j += batchSize {
				ncs := &proto.NamedChangeSet{}
				cs := &proto.ChangeSet{}
				cs.Pairs = []*proto.KVPair{}

				batchEnd := j + batchSize
				if batchEnd > end {
					batchEnd = end
				}

				// Add key-value pairs to the batch up to batchSize
				for k := j; k < batchEnd; k++ {
					kv := allKVs[k]
					// No store key for benchmarks
					cs.Pairs = append(cs.Pairs, &proto.KVPair{
						Key:   kv.Key,
						Value: kv.Value,
					})
				}
				ncs.Changeset = *cs
				startTime := time.Now()
				err := db.ApplyChangesetSync(version, []*proto.NamedChangeSet{ncs})
				latency := time.Since(startTime)

				if err == nil {
					latencies <- latency
				} else {
					panic(err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(latencies)

	for l := range latencies {
		allLatencies = append(allLatencies, l)
	}

	return allLatencies
}
