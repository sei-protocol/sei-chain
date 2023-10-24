package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (

	// TODO: Will include rocksdb, pebbledb and sqlite in future PR's
	ValidDBBackends = map[string]bool{}
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "seidb",
		Short: "A tool to generate raw key value data from a node as well as benchmark different backends",
	}

	rootCmd.AddCommand(GenerateCmd(), BenchmarkWriteCmd(), BenchmarkReadCmd(), BenchmarkDBIterationCmd(), BenchmarkDBReverseIterationCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
