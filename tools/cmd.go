package tools

import (
	"github.com/spf13/cobra"

	scanner "github.com/sei-protocol/sei-chain/tools/tx-scanner/cmd"
)

func ToolCmd() *cobra.Command {
	toolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "A set of useful tools for sei chain",
	}
	toolsCmd.AddCommand(scanner.ScanCmd())
	return toolsCmd
}
