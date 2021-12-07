package cli

import (
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the query commands for the ICA controller submodule
func GetQueryCmd() *cobra.Command {
	queryCmd := &cobra.Command{
		Use:                        "controller",
		Short:                      "interchain-accounts controller subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
	}

	queryCmd.AddCommand(
		GetCmdParams(),
	)

	return queryCmd
}
