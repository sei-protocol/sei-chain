package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/tools/migration/sc"
	"github.com/sei-protocol/sei-chain/tools/migration/ss"
	"github.com/sei-protocol/sei-chain/tools/migration/utils"
	"github.com/sei-protocol/sei-db/config"
	sstypes "github.com/sei-protocol/sei-db/ss"
	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

func MigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-iavl",
		Short: "A tool to migrate full IAVL data store to SeiDB. Use this tool to migrate IAVL to SeiDB SC and SS database.",
		Run:   execute,
	}
	cmd.PersistentFlags().String("home-dir", "/root/.sei", "Sei home directory")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	dataDir := filepath.Join(homeDir, "data")
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		panic(err)
	}
	latestVersion := rootmulti.GetLatestVersion(db)
	fmt.Printf("latest version: %d\n", latestVersion)

	if err = migrateSC(latestVersion, homeDir, db); err != nil {
		panic(err)
	}
}

func migrateSC(version int64, homeDir string, db dbm.DB) error {
	migrator := sc.NewMigrator(homeDir, db)
	return migrator.Migrate(version)
}

func VerifyMigrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-migration",
		Short: "A tool to verify migration of a IAVL data store to SeiDB at a particular height.",
		Run:   verify,
	}

	cmd.PersistentFlags().Int64("version", -1, "Version to run migration verification on")
	cmd.PersistentFlags().String("home-dir", "/root/.sei", "Sei home directory")

	return cmd
}

func verify(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	version, _ := cmd.Flags().GetInt64("version")

	fmt.Printf("version %d\n", version)

	if version <= 0 {
		panic("Must specify version for verification")
	}

	dataDir := filepath.Join(homeDir, "data")
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		panic(err)
	}

	err = verifySS(version, homeDir, db)
	if err != nil {
		fmt.Printf("Verification Failed with err: %s\n", err.Error())
		return
	}

	fmt.Println("Verification Succeeded")
}

func verifySS(version int64, homeDir string, db dbm.DB) error {
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Enable = true

	stateStore, err := sstypes.NewStateStore(log.NewNopLogger(), homeDir, ssConfig)
	if err != nil {
		return err
	}

	migrator := ss.NewMigrator(db, stateStore)
	return migrator.Verify(version)
}

func GenerateStats() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "iavl-stats",
		Short: "A tool to generate archive node iavl stats like number of keys and size per module.",
		Run:   generateIavlStats,
	}
	cmd.PersistentFlags().String("home-dir", "/root/.sei", "Sei home directory")
	return cmd
}

func generateIavlStats(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	dataDir := filepath.Join(homeDir, "data")
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		panic(err)
	}

	fmt.Println("Aggregating iavl module stats...")
	for _, module := range utils.Modules {

		startTimeModule := time.Now()
		fmt.Printf("Aggregating stats for module %s...\n", module)

		// Prepare the prefixed DB for the module
		prefixDB := dbm.NewPrefixDB(db, []byte(utils.BuildRawPrefix(module)))

		// Get an iterator for the prefixed DB
		itr, err := prefixDB.Iterator(nil, nil)
		if err != nil {
			panic(fmt.Errorf("failed to create iterator: %w", err))
		}
		defer itr.Close()

		// Aggregated stats for the current module
		var totalKeys int
		var totalKeySize int
		var totalValueSize int

		for ; itr.Valid(); itr.Next() {
			value := bytes.Clone(itr.Value())
			node, err := iavl.MakeNode(value)
			if err != nil {
				panic(fmt.Errorf("failed to make node: %w", err))
			}

			// Only export leaf nodes
			if node.GetHeight() == 0 {
				totalKeys++
				totalKeySize += len(itr.Key())
				totalValueSize += len(itr.Value())
				if totalKeys%1000000 == 0 {
					fmt.Printf("Module %s num of keys %d, key size %d, value size %d\n",
						module, totalKeys, totalKeySize, totalValueSize)
				}
			}
		}

		if err := itr.Error(); err != nil {
			panic(fmt.Errorf("iterator error for module %s: %w", module, err))
		}

		moduleDuration := time.Since(startTimeModule)
		fmt.Printf("Module %s stats finished, total keys %d, total key size %d, total value size %d, time taken %s\n",
			module, totalKeys, totalKeySize, totalValueSize, moduleDuration)
	}

	fmt.Println("SeiDB Archive Migration: Aggregation completed.")
}
