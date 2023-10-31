package tools

import (
	tx_scanner "github.com/sei-protocol/sei-chain/tools/tx-scanner"
	"github.com/spf13/cobra"
)

func ToolCmd() *cobra.Command {
	toolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "A set of useful tools for sei chain",
	}
	toolsCmd.AddCommand(tx_scanner.ScanCmd())
	return toolsCmd
}
