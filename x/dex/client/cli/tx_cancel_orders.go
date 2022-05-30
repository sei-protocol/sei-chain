package cli

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ = strconv.Itoa(0)

func CmdCancelOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel-orders [contract address] [orders...]",
		Short: "Bulk cancel orders",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			if err != nil {
				return err
			}
			orderCancellations := []*types.OrderCancellation{}
			for _, order := range args[1:] {
				orderCancellation := types.OrderCancellation{}
				orderDetails := strings.Split(order, ",")
				orderCancellation.PositionDirection = types.PositionDirection(
					types.PositionDirection_value[orderDetails[0]],
				)
				argPrice, err := sdk.NewDecFromStr(orderDetails[1])
				if err != nil {
					return err
				}
				orderCancellation.Price = argPrice
				argQuantity, err := sdk.NewDecFromStr(orderDetails[2])
				if err != nil {
					return err
				}
				orderCancellation.Quantity = argQuantity
				orderCancellation.PriceDenom = types.Denom(types.Denom_value[orderDetails[3]])
				orderCancellation.AssetDenom = types.Denom(types.Denom_value[orderDetails[4]])
				orderCancellation.PositionEffect = types.PositionEffect(
					types.PositionEffect_value[orderDetails[5]],
				)
				argLeverage, err := sdk.NewDecFromStr(orderDetails[6])
				if err != nil {
					return err
				}
				orderCancellation.Leverage = argLeverage
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
