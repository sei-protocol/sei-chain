package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/tools/hash_verification/iavl"
	"github.com/sei-protocol/sei-chain/tools/hash_verification/pebbledb"
	"github.com/sei-protocol/sei-db/config"
	sstypes "github.com/sei-protocol/sei-db/ss"
	"github.com/tendermint/tendermint/libs/log"
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

func GeneratePebbleHashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate-pebble-hash",
		Short: "A tool to scan full Pebble archive database and generate a hash for every N blocks per module",
		Run:   generatePebbleHash,
	}
	cmd.PersistentFlags().String("home-dir", "/root/.sei", "Sei home directory")
	cmd.PersistentFlags().Int64("blocks-interval", 1_000_000, "Generate a hash every N blocks")
	return cmd
}

func generatePebbleHash(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	blocksInterval, _ := cmd.Flags().GetInt64("blocks-interval")

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Enable = true
	ssConfig.KeepRecent = 0
	stateStore, err := sstypes.NewStateStore(log.NewNopLogger(), homeDir, ssConfig)

	if err != nil {
		panic(err)
	}

	scanner := pebbledb.NewHashScanner(stateStore, blocksInterval)
	scanner.ScanAllModules()
}
