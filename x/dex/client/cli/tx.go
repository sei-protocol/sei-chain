package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	// "github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

var DefaultRelativePacketTimeoutTimestamp = uint64((time.Duration(10) * time.Minute).Nanoseconds())

//nolint:deadcode,unused // I assume we'll use this later.
const (
	flagPacketTimeoutTimestamp = "packet-timeout-timestamp"
	listSeparator              = ","
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdPlaceOrders())
	cmd.AddCommand(CmdCancelOrders())
	cmd.AddCommand(CmdLiquidate())
	cmd.AddCommand(CmdRegisterContract())
	cmd.AddCommand(NewRegisterPairsProposalTxCmd())
	cmd.AddCommand(NewUpdateTickSizeProposalTxCmd())
	cmd.AddCommand(NewAddAssetProposalTxCmd())
	// this line is used by starport scaffolding # 1

	return cmd
}
