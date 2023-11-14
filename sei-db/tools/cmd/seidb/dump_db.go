package main

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/sei-protocol/sei-db/ss"
	"github.com/spf13/cobra"
)

func DumpDbCmd() *cobra.Command {
	dumpDbCmd := &cobra.Command{
		Use:   "dump-db",
		Short: "For a given State Store DB, dump-db iterates over all keys and values for a specific store and writes them to a file",
		Run:   dump,
	}

	dumpDbCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	dumpDbCmd.PersistentFlags().StringP("module", "m", "", "Module to export")
	dumpDbCmd.PersistentFlags().StringP("db-backend", "d", "", "DB Backend")

	return dumpDbCmd
}

func dump(cmd *cobra.Command, _ []string) {
	outputDir, _ := cmd.Flags().GetString("output-dir")
	module, _ := cmd.Flags().GetString("module")
	dbBackend, _ := cmd.Flags().GetString("db-backend")

	if dbBackend == "" {
		panic("Must provide db backend when benchmarking")
	}

	_, isAcceptedBackend := ValidDBBackends[dbBackend]
	if !isAcceptedBackend {
		panic(fmt.Sprintf("Unsupported db backend: %s\n", dbBackend))
	}

	if outputDir == "" {
		panic("Must provide output dir when generating db export")
	}

	if module == "" {
		panic("Must provide module to export")
	}

	DumpDbData(dbBackend, module, outputDir)
}

// Outputs the raw keys and values for all modules at a height to a file
func DumpDbData(dbBackend string, module string, outputDir string) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}

	backend, err := ss.NewStateStoreDB(outputDir, ss.BackendType(dbBackend))
	if err != nil {
		panic(err)
	}

	err = backend.DebugIterateStore(module, outputDir)
	if err != nil {
		panic(err)
	}
}
