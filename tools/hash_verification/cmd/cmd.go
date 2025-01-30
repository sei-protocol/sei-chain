package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/tools/hash_verification/iavl"
	dbm "github.com/tendermint/tm-db"
)

func GenerateIavlHashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate-iavl-hash",
		Short: "A tool to scan full IAVL archive database and generate a hash for every N blocks per module",
		Run:   generateIavlHash,
	}
	cmd.PersistentFlags().String("home-dir", "/root/.sei", "Sei home directory")
	cmd.PersistentFlags().Int64("blocks-interval", 1_000_000, "Generate a hash every N blocks")
	return cmd
}

func generateIavlHash(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	blocksInterval, _ := cmd.Flags().GetInt64("blocks-interval")
	dataDir := filepath.Join(homeDir, "data")
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		panic(err)
	}
	scanner := iavl.NewHashScanner(db, blocksInterval)
	scanner.ScanAllModules()
}
