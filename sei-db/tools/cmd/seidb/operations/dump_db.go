package operations

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/sei-protocol/sei-db/tools/cmd/seidb/benchmark"
	"github.com/sei-protocol/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

const outputFileName = "db_dump.kv"

func DumpDbCmd() *cobra.Command {
	dumpDbCmd := &cobra.Command{
		Use:   "dump-db",
		Short: "For a given State Store DB, dump-db iterates over all keys and values for a specific store and writes them to a file",
		Run:   executeDumpDB,
	}

	dumpDbCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	dumpDbCmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	// TODO: Accept multiple modules. Can pass empty to iterate over all stores
	dumpDbCmd.PersistentFlags().StringP("module", "m", "", "Module to export. Leave empty to export all")
	dumpDbCmd.PersistentFlags().StringP("db-backend", "b", "", "DB Backend")

	return dumpDbCmd
}

func executeDumpDB(cmd *cobra.Command, _ []string) {
	outputDir, _ := cmd.Flags().GetString("output-dir")
	module, _ := cmd.Flags().GetString("module")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	dbBackend, _ := cmd.Flags().GetString("db-backend")

	if dbDir == "" {
		panic("Must provide database dir")
	}

	if dbBackend == "" {
		panic("Must provide db backend")
	}

	_, isAcceptedBackend := benchmark.ValidDBBackends[dbBackend]
	if !isAcceptedBackend {
		panic(fmt.Sprintf("Unsupported db backend: %s\n", dbBackend))
	}

	if outputDir == "" {
		panic("Must provide output dir when generating db export")
	}

	DumpDbData(dbBackend, module, outputDir, dbDir)
}

// Outputs the raw keys and values for all modules at a height to a file
func DumpDbData(dbBackend string, module string, outputDir string, dbDir string) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}

	// Create output file
	currentFile, err := utils.CreateFile(outputDir, outputFileName)
	if err != nil {
		panic(err)
	}
	defer currentFile.Close()

	// TODO: Defer Close Db
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = dbBackend
	backend, err := ss.NewStateStore(logger.NewNopLogger(), outputDir, ssConfig)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Writing db data to %s...\n", outputFileName)

	// Callback to write db entries to file
	_, err = backend.RawIterate(module, func(key, value []byte, version int64) bool {
		_, err = currentFile.WriteString(fmt.Sprintf("Key: %X Val: %X Version: %d\n", key, value, version))
		if err != nil {
			panic(err)
		}
		return false
	})
	if err != nil {
		panic(err)
	}
}
