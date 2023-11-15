package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const RocksDBBackendName = "rocksdb"
const PebbleDBBackendName = "pebbledb"

var (

	// TODO: Will include rocksdb, pebbledb and sqlite in future PR's
	ValidDBBackends = map[string]bool{
		RocksDBBackendName:  true,
		PebbleDBBackendName: true,
	}
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "seidb",
		Short: "A tool to generate raw key value data from a node as well as benchmark different backends",
	}

	rootCmd.AddCommand(GenerateCmd(), BenchmarkWriteCmd(), BenchmarkReadCmd(), BenchmarkDBIterationCmd(), BenchmarkDBReverseIterationCmd(), DumpDbCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
