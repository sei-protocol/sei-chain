package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	"github.com/sei-protocol/sei-chain/tools/migration/sc"
	"github.com/sei-protocol/sei-chain/tools/migration/ss"
	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"
)

func MigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-iavl",
		Short: "A tool to migrate full IAVL data store to SeiDB. Use this tool to migrate IAVL to SeiDB SC and SS database.",
		Run:   execute,
	}
	cmd.PersistentFlags().String("home-dir", "~/.sei", "Sei home directory")
	cmd.PersistentFlags().String("target-db", "SS", "Available options: [SS, SC]")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	target, _ := cmd.Flags().GetString("target-db")
	dataDir := filepath.Join(homeDir, "data")
	fmt.Printf("dataDir %+v\n", dataDir)
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		panic(err)
	}
	latestVersion := rootmulti.GetLatestVersion(db)
	fmt.Printf("target %+v homeDir %+v dataDir %+v latestVersion %+v\n", target, homeDir, dataDir, latestVersion)
	if target == "SS" {
		migrateSS(latestVersion, homeDir, db)
	} else if target == "SC" {
		migrateSC(latestVersion, homeDir, db)
	} else {
		panic("Invalid target DB, either SS or SC should be provided")
	}
}

func migrateSC(version int64, homeDir string, db dbm.DB) error {
	migrator := sc.NewMigrator(homeDir, db)
	return migrator.Migrate(version, homeDir)
}

func migrateSS(version int64, homeDir string, db dbm.DB) error {
	migrator := ss.NewMigrator(homeDir, db)
	return migrator.Migrate(version, homeDir)
}
