package operations

import (
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func DumpFlatKVCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-flatkv",
		Short: "Iterate and dump all FlatKV data (account, code, storage, legacy DBs)",
		Run:   executeDumpFlatKV,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV data directory (e.g. /root/.sei/data/committer.db/flatkv)")
	cmd.PersistentFlags().StringP("output-dir", "o", "", "Output directory for dump files")
	cmd.PersistentFlags().Int64("height", 0, "Block height (0 = latest)")
	return cmd
}

func executeDumpFlatKV(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	height, _ := cmd.Flags().GetInt64("height")

	if dbDir == "" {
		panic("Must provide database dir")
	}
	if outputDir == "" {
		panic("Must provide output dir")
	}

	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	fmt.Printf("Opened FlatKV at version %d, root hash: %X\n", store.Version(), store.RootHash())

	if err := dumpFlatKVData(store.CommitStore, outputDir); err != nil {
		panic(err)
	}
}

func dumpFlatKVData(store *flatkv.CommitStore, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	outFile, err := utils.CreateFile(outputDir, "flatkv.dump")
	if err != nil {
		return err
	}
	defer func() { _ = outFile.Close() }()

	_, _ = fmt.Fprintf(outFile, "# FlatKV dump: version=%d, rootHash=%X\n",
		store.Version(), store.RootHash())

	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	if !iter.First() {
		if err := iter.Error(); err != nil {
			return fmt.Errorf("iterator error: %w", err)
		}
		fmt.Println("FlatKV store is empty")
		return nil
	}

	var totalKeys uint64
	for iter.Valid() {
		_, err := fmt.Fprintf(outFile, "Key: %X, Value: %X\n", iter.Key(), iter.Value())
		if err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		totalKeys++

		if totalKeys%1000000 == 0 {
			fmt.Printf("  dumped %d keys...\n", totalKeys)
		}

		iter.Next()
	}

	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterator error: %w", err)
	}

	fmt.Printf("Total: %d keys dumped to %s/flatkv.dump\n", totalKeys, outputDir)
	return nil
}
