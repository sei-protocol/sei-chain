package main

import (
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/tools/cmd/seidb/benchmark"
	"github.com/sei-protocol/sei-chain/sei-db/tools/cmd/seidb/operations"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "seidb",
		Short: "A tool to generate raw key value data from a node as well as benchmark different backends",
	}

	rootCmd.AddCommand(
		benchmark.GenerateCmd(),
		benchmark.DBWriteCmd(),
		benchmark.DBRandomReadCmd(),
		benchmark.DBIterationCmd(),
		benchmark.DBReverseIterationCmd(),
		operations.DumpDbCmd(),
		operations.InspectCmd(),
		operations.PruneCmd(),
		operations.DumpIAVLCmd(),
		operations.DumpFlatKVCmd(),
		operations.StateSizeCmd(),
		operations.MemiavlLatestVersionCmd(),
		operations.ImportFlatKVFromMemiavlCmd(),
		operations.ReplayChangelogCmd(),
		operations.TraceProfileReportCmd(),
		operations.MigrateEvmStatusCmd(),
		operations.EvmLogicalDigestCmd(),
		operations.HashLogCmd())
	if err := rootCmd.Execute(); err != nil {
		// Subcommands with a --json mode make stdout a machine-readable channel, so a
		// bare error line there would corrupt the report a caller is parsing.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
