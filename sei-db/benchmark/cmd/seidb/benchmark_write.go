package main

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/sei-protocol/sei-db/benchmark/dbbackend"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/spf13/cobra"
)

func BenchmarkWriteCmd() *cobra.Command {
	benchmarkWriteCmd := &cobra.Command{
		Use:   "benchmark-write",
		Short: "Benchmark write is designed to measure write performance of different db backends",
		Run:   benchmarkWrite,
	}

	benchmarkWriteCmd.PersistentFlags().StringP("db-backend", "d", "", "DB Backend")
	benchmarkWriteCmd.PersistentFlags().StringP("raw-kv-input-dir", "r", "", "Input Directory for benchmark which contains the raw kv data")
	benchmarkWriteCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	benchmarkWriteCmd.PersistentFlags().IntP("concurrency", "c", 1, "Concurrency while writing to db")
	benchmarkWriteCmd.PersistentFlags().IntP("batch-size", "b", 1, "batch size for db writes")
	benchmarkWriteCmd.PersistentFlags().IntP("num-versions", "v", 1, "number of versions in db")

	return benchmarkWriteCmd
}

func benchmarkWrite(cmd *cobra.Command, args []string) {
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

	BenchmarkWrite(rawKVInputDir, numVersions, outputDir, dbBackend, concurrency, batchSize)
}

// BenchmarkWrite write latencies and throughput of db backend
func BenchmarkWrite(inputKVDir string, numVersions int, outputDir string, dbBackend string, concurrency int, batchSize int) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}
	// Iterate over files in directory
	fmt.Printf("Reading Raw Keys and Values from %s\n", inputKVDir)

	backend, err := ss.NewStateStoreDB(outputDir, ss.BackendType(dbBackend))
	if err != nil {
		panic(err)
	}
	dbbackend.BenchmarkDBWrite(backend, inputKVDir, numVersions, concurrency, batchSize)
	backend.Close()
}
