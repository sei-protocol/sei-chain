package tools

import (
	migration "github.com/sei-protocol/sei-chain/tools/migration/cmd"
	scanner "github.com/sei-protocol/sei-chain/tools/tx-scanner/cmd"
	"github.com/spf13/cobra"
)

func ToolCmd() *cobra.Command {
	toolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "A set of useful tools for sei chain",
	}
	toolsCmd.AddCommand(scanner.ScanCmd())
	toolsCmd.AddCommand(migration.MigrateCmd())
	return toolsCmd
}
