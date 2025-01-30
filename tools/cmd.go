package tools

import (
	"github.com/spf13/cobra"

	hasher "github.com/sei-protocol/sei-chain/tools/hash_verification/cmd"
	migration "github.com/sei-protocol/sei-chain/tools/migration/cmd"
	scanner "github.com/sei-protocol/sei-chain/tools/tx-scanner/cmd"
)

func ToolCmd() *cobra.Command {
	toolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "A set of useful tools for sei chain",
	}
	toolsCmd.AddCommand(scanner.ScanCmd())
	toolsCmd.AddCommand(migration.MigrateCmd())
	toolsCmd.AddCommand(migration.VerifyMigrationCmd())
	toolsCmd.AddCommand(migration.GenerateStats())
	toolsCmd.AddCommand(hasher.GenerateIavlHashCmd())
	return toolsCmd
}
