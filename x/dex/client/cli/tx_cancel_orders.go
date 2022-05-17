package cli

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdCancelOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel-orders [contract address] [nonce] [orders...]",
		Short: "Bulk cancel orders",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			argNonce, err := cast.ToUint64E(args[1])
			if err != nil {
				return err
			}
			orderCancellations := []*types.OrderCancellation{}
			for _, order := range args[2:] {
				orderCancellation := types.OrderCancellation{}
				orderDetails := strings.Split(order, ",")
				orderCancellation.Long = orderDetails[0] == "Long"
				argPrice, err := cast.ToUint64E(orderDetails[1])
				if err != nil {
					return err
				}
				orderCancellation.Price = argPrice
				argQuantity, err := cast.ToUint64E(orderDetails[2])
				if err != nil {
					return err
				}
				orderCancellation.Quantity = argQuantity
				orderCancellation.PriceDenom = orderDetails[3]
				orderCancellation.AssetDenom = orderDetails[4]
				orderCancellation.Open = orderDetails[5] == "Open"
				orderCancellation.Leverage = orderDetails[6]
				orderCancellations = append(orderCancellations, &orderCancellation)
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgCancelOrders(
				clientCtx.GetFromAddress().String(),
				orderCancellations,
				argContractAddr,
				argNonce,
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
