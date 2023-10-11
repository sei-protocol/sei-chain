package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/sei-protocol/sei-db/benchmark/dbbackend"
	"github.com/sei-protocol/sei-db/benchmark/utils"
)

var exportModules = []string{
	"dex", "wasm", "accesscontrol", "oracle", "epoch", "mint", "acc", "bank", "crisis", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
}

// TODO: Add all the other backends when compatible
const rocksDBBackend = "rocksDB"

var validDBBackends = map[string]bool{rocksDBBackend: true}

func main() {
	var command string
	var levelDBDir string
	var modules string
	var outputDir string
	var dbBackend string
	var rawKVInputDir string
	var version int
	var concurrency int
	var maxRetries int
	flag.StringVar(&command, "command", "", "generate or benchmark")
	flag.StringVar(&levelDBDir, "leveldb-dir", "", "level db dir")
	flag.StringVar(&modules, "modules", "", "Modules to export")
	flag.StringVar(&outputDir, "output-dir", "", "Output Directory")
	flag.StringVar(&dbBackend, "db-backend", "", "DB Backend")
	flag.StringVar(&rawKVInputDir, "raw-kv-input-dir", "", "Input Directory for benchmark which contains the raw kv data")
	flag.IntVar(&version, "version", 0, "db version")
	flag.IntVar(&concurrency, "concurrency", 1, "Concurrency while writing to db")
	flag.IntVar(&maxRetries, "max-retries", 0, "Max Retries while writing to db")
	flag.Parse()

	if command == "" {
		panic("Need to provide a command: either generate or benchmark")
	}

	// Generate uses the iavl viewer logic to write out the raw keys and values from the kb for each module
	if command == "generate" {
		// Check necessary args
		if levelDBDir == "" {
			panic("Must provide leveldb dir when generating raw kv data")
		}

		if outputDir == "" {
			panic("Must provide output dir when generating raw kv data")
		}

		if modules != "" {
			exportModules = strings.Split(modules, ",")
		}
		GenerateData(levelDBDir, exportModules, outputDir, version)
	}

	// Benchmark is designed to measure read, write, iterate, etc. performance of different db backends (e.g. rocksdb)
	if command == "benchmark-write" {
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
}

// Outputs the raw keys and values for all modules at a height to a file
func GenerateData(dbDir string, modules []string, outputDir string, version int) {
	// Generate raw kv data for each module
	for _, module := range modules {
		fmt.Printf("Generating Raw Keys and Values for %s module at version %d\n", module, version)

		modulePrefix := fmt.Sprintf("s/k:%s/", module)
		tree, err := utils.ReadTree(dbDir, version, []byte(modulePrefix))
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

		outputFileName := fmt.Sprintf("%s/%s.kv", outputDir, module)
		utils.WriteTreeDataToFile(tree, outputFileName)
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
