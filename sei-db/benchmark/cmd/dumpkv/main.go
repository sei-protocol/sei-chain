package main

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-db/benchmark/dbbackend"
	"github.com/sei-protocol/sei-db/benchmark/utils"
)

const rocksDBBackend = "rocksDB"

var (
	levelDBDir    string
	modules       string
	outputDir     string
	dbBackend     string
	rawKVInputDir string
	version       int
	concurrency   int
	maxRetries    int
	chunkSize     int
	exportModules = []string{
		"dex", "wasm", "accesscontrol", "oracle", "epoch", "mint", "acc", "bank", "crisis", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
	}
	validDBBackends = map[string]bool{
		rocksDBBackend: true,
	}

	rootCmd = &cobra.Command{
		Use:   "dumpkv",
		Short: "A tool to generate raw key value data from a node as well as benchmark different backends",
	}

	generateCmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate uses the iavl viewer logic to write out the raw keys and values from the kb for each module",
		Run:   generate,
	}

	benchmarkCmd = &cobra.Command{
		Use:   "benchmark-write",
		Short: "Benchmark is designed to measure read, write, iterate, etc. performance of different db backends",
		Run:   benchmark,
	}
)

func init() {
	rootCmd.AddCommand(generateCmd, benchmarkCmd)

	generateCmd.Flags().StringVar(&levelDBDir, "leveldb-dir", "", "level db dir")
	generateCmd.Flags().StringVar(&outputDir, "output-dir", "", "Output Directory")
	generateCmd.Flags().StringVar(&modules, "modules", "", "Modules to export")
	generateCmd.Flags().IntVar(&version, "version", 0, "db version")
	generateCmd.Flags().IntVar(&chunkSize, "chunkSize", 100, "chunk size for each kv file")

	benchmarkCmd.Flags().StringVar(&dbBackend, "db-backend", "", "DB Backend")
	benchmarkCmd.Flags().StringVar(&rawKVInputDir, "raw-kv-input-dir", "", "Input Directory for benchmark which contains the raw kv data")
	benchmarkCmd.Flags().StringVar(&outputDir, "output-dir", "", "Output Directory")
	benchmarkCmd.Flags().IntVar(&concurrency, "concurrency", 1, "Concurrency while writing to db")
	benchmarkCmd.Flags().IntVar(&maxRetries, "max-retries", 0, "Max Retries while writing to db")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}

func generate(cmd *cobra.Command, args []string) {
	if levelDBDir == "" {
		panic("Must provide leveldb dir when generating raw kv data")
	}

	if outputDir == "" {
		panic("Must provide output dir when generating raw kv data")
	}

	if modules != "" {
		exportModules = strings.Split(modules, ",")
	}
	GenerateData(levelDBDir, exportModules, outputDir, version, chunkSize)
}

func benchmark(cmd *cobra.Command, args []string) {
	if dbBackend == "" {
		panic("Must provide db backend when benchmarking")
	}

	if rawKVInputDir == "" {
		panic("Must provide raw kv input dir when benchmarking")
	}

	if outputDir == "" {
		panic("Must provide output dir when generating raw kv data")
	}

	_, isAcceptedBackend := validDBBackends[dbBackend]
	if !isAcceptedBackend {
		panic(fmt.Sprintf("Unsupported db backend: %s\n", dbBackend))
	}

	if modules != "" {
		exportModules = strings.Split(modules, ",")
	}

	BenchmarkWrite(rawKVInputDir, exportModules, outputDir, dbBackend, concurrency, maxRetries)
}

// Outputs the raw keys and values for all modules at a height to a file
func GenerateData(dbDir string, modules []string, outputDir string, version int, chunkSize int) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}
	// Generate raw kv data for each module
	db, err := utils.OpenDB(dbDir)
	if err != nil {
		panic(err)
	}
	for _, module := range modules {
		fmt.Printf("Generating Raw Keys and Values for %s module at version %d\n", module, version)

		modulePrefix := fmt.Sprintf("s/k:%s/", module)
		tree, err := utils.ReadTree(db, version, []byte(modulePrefix))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading data: %s\n", err)
			return
		}
		treeHash, err := tree.Hash()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing tree: %s\n", err)
			return
		}

		fmt.Printf("Tree hash is %X, tree size is %d\n", treeHash, tree.ImmutableTree().Size())

		outputFileNamePattern := fmt.Sprintf("%s/%s", outputDir, module)
		utils.WriteTreeDataToFile(tree, outputFileNamePattern, chunkSize)
	}
}

// Benchmark write latencies and throughput of db backend
func BenchmarkWrite(dbDir string, modules []string, outputDir string, dbBackend string, concurrency int, maxRetries int) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}
	// Iterate over files in directory
	for _, module := range modules {
		exportedKVFile := fmt.Sprintf("%s/%s.kv", dbDir, module)
		fmt.Printf("Reading Raw Keys and Values from %s\n", exportedKVFile)

		if dbBackend == rocksDBBackend {
			backend := dbbackend.RocksDBBackend{}
			backend.BenchmarkDBWrite(exportedKVFile, outputDir, concurrency, maxRetries)
		}
	}
	return
}
