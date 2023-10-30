package main

import (
	"fmt"

	"github.com/sei-protocol/sei-db/benchmark/dbbackend"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/spf13/cobra"
)

func BenchmarkDBIterationCmd() *cobra.Command {
	benchmarkForwardIterationCmd := &cobra.Command{
		Use:   "benchmark-iteration",
		Short: "Benchmark iteration is designed to measure forward iteration performance of different db backends",
		Run:   benchmarkForwardIteration,
	}

	benchmarkForwardIterationCmd.PersistentFlags().StringP("db-backend", "d", "", "DB Backend")
	benchmarkForwardIterationCmd.PersistentFlags().StringP("raw-kv-input-dir", "r", "", "Input Directory for benchmark which contains the raw kv data")
	benchmarkForwardIterationCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	benchmarkForwardIterationCmd.PersistentFlags().IntP("concurrency", "c", 1, "Concurrency while writing to db")
	benchmarkForwardIterationCmd.PersistentFlags().Int64P("max-operations", "p", 1000, "Max operations to run")
	benchmarkForwardIterationCmd.PersistentFlags().IntP("num-versions", "v", 1, "number of versions in db")
	benchmarkForwardIterationCmd.PersistentFlags().IntP("iteration-steps", "i", 10, "Number of steps to run per iteration")

	return benchmarkForwardIterationCmd
}

func benchmarkForwardIteration(cmd *cobra.Command, args []string) {
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

	BenchmarkDBIteration(rawKVInputDir, numVersions, outputDir, dbBackend, concurrency, maxOps, iterationSteps)
}

// BenchmarkDBIteration read latencies and throughput of db backend
func BenchmarkDBIteration(inputKVDir string, numVersions int, outputDir string, dbBackend string, concurrency int, maxOps int64, iterationSteps int) {
	// Iterate over db at directory
	fmt.Printf("Iterating Over DB at  %s\n", outputDir)

	backend, err := ss.NewStateStoreDB(outputDir, ss.BackendType(dbBackend))
	if err != nil {
		panic(err)
	}
	dbbackend.BenchmarkDBForwardIteration(backend, inputKVDir, numVersions, concurrency, maxOps, iterationSteps)
	backend.Close()
}
