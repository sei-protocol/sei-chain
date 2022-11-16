package tx

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cutils "github.com/sei-protocol/sei-chain/x/dex/client/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdUpdateTickSize() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-tick-size [update-tick-size-file]",
		Short: "Update tick size for a market",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			tickTx, err := cutils.ParseUpdateTickSizeTxJSON(clientCtx.LegacyAmino, args[0])
			if err != nil {
				return err
			}

			txTick, err := tickTx.TickSizes.ToTickSizes()
			if err != nil {
				return err
			}

			msg := types.NewMsgUpdateTickSize(
				clientCtx.GetFromAddress().String(),
				txTick,
			)
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
