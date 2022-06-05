package cli

import (
	"errors"
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
				argPositionDir, err := types.GetPositionDirectionFromStr(orderDetails[0])
				if err != nil {
					return err
				}
				orderCancellation.PositionDirection = argPositionDir
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
				reqPriceDenom, unit, err := types.GetDenomFromStr(orderDetails[3])
				if err != nil {
					return err
				}
				if unit != types.Unit_STANDARD {
					return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
				}
				reqAssetDenom, unit, err := types.GetDenomFromStr(orderDetails[4])
				if err != nil {
					return err
				}
				if unit != types.Unit_STANDARD {
					return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
				}
				orderCancellation.PriceDenom = reqPriceDenom
				orderCancellation.AssetDenom = reqAssetDenom
				argPositionEffect, err := types.GetPositionEffectFromStr(orderDetails[5])
				if err != nil {
					return err
				}
				orderCancellation.PositionEffect = argPositionEffect
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
