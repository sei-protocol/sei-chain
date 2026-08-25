package cli

import (
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"

	ibcclient "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client"
	channel "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel"
	host "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	ibcTxCmd := &cobra.Command{
		Use:                        host.ModuleName,
		Short:                      "IBC transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	ibcTxCmd.AddCommand(
		ibcclient.GetTxCmd(),
		channel.GetTxCmd(),
	)

	return ibcTxCmd
}
