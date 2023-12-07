package main

import (
	"fmt"
	"os"

	"github.com/sei-protocol/sei-db/tools/cmd/benchmark"
	"github.com/sei-protocol/sei-db/tools/cmd/operations"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "seidb",
		Short: "A tool to generate raw key value data from a node as well as benchmark different backends",
	}

	rootCmd.AddCommand(
		benchmark.GenerateCmd(),
		benchmark.BenchmarkWriteCmd(),
		benchmark.BenchmarkReadCmd(),
		benchmark.BenchmarkDBIterationCmd(),
		benchmark.BenchmarkDBReverseIterationCmd(),
		operations.DumpDbCmd(),
		operations.PruneCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
