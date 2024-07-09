package cmd

import (
	"github.com/sei-protocol/sei-chain/tools/migration/sc"
	"github.com/spf13/cobra"
)

func MigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-iavl",
		Short: "A tool to migrate full IAVL data store to SeiDB",
		Run:   execute,
	}
	cmd.PersistentFlags().Int64("height", 0, "Start height")
	cmd.PersistentFlags().String("home-dir", "~/.sei", "Sei home directory")
	cmd.PersistentFlags().String("target", "SS", "Whether to migrate SS or SC")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
	homeDir, _ := cmd.Flags().GetString("home-dir")
	height, _ := cmd.Flags().GetInt64("height")
	target, _ := cmd.Flags().GetString("target")
	if target == "SS" {
		migrateSS(uint64(height), homeDir)
	} else if target == "SC" {
		migrateSC(uint64(height), homeDir)
	}
}

func migrateSC(height uint64, homeDir string) error {
	migrator := sc.NewMigrator(homeDir)
	return migrator.Migrate(height)
}

func migrateSS(height uint64, homeDir string) {

}
