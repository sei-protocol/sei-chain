package cli

import (
	"errors"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdCancelOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel-orders [contract address] [ids...]",
		Short: "Bulk cancel orders",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			if err != nil {
				return err
			}
			idsToCancel := []uint64{}
			for _, idStr := range args[1:] {
				id, err := strconv.ParseUint(idStr, 10, 64)
				if err != nil {
					return errors.New("invalid order ID")
				}
				idsToCancel = append(idsToCancel, id)
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgCancelOrders(
				clientCtx.GetFromAddress().String(),
				idsToCancel,
				argContractAddr,
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
