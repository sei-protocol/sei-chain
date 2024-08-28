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
	cmd.PersistentFlags().String("home-dir", "/root/.sei", "Sei home directory")
	cmd.PersistentFlags().String("target-db", "", "Available options: [SS, SC]")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	target, _ := cmd.Flags().GetString("target-db")
	dataDir := filepath.Join(homeDir, "data")
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		panic(err)
	}
	latestVersion := rootmulti.GetLatestVersion(db)
	fmt.Printf("latest version: %d\n", latestVersion)
	if target == "SS" {
		if err = migrateSS(latestVersion, homeDir, db); err != nil {
			panic(err)
		}
	} else if target == "SC" {
		if err = migrateSC(latestVersion, homeDir, db); err != nil {
			panic(err)
		}
	} else {
		panic("Invalid target-db, either SS or SC should be provided")
	}
}

func migrateSC(version int64, homeDir string, db dbm.DB) error {
	migrator := sc.NewMigrator(homeDir, db)
	return migrator.Migrate(version)
}

func migrateSS(version int64, homeDir string, db dbm.DB) error {
	migrator := ss.NewMigrator(homeDir, db)
	return migrator.Migrate(version, homeDir)
}

func verifySS(version int64, homeDir string, db dbm.DB) error {
	migrator := ss.NewMigrator(homeDir, db)
	return migrator.Verify(version)
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
