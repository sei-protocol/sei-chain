package cli

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

const (
	flagAmount = "amount"
)

func CmdPlaceOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "place-orders [contract address] [orders...] --amount [coins,optional]",
		Short: "Bulk place orders",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			orderPlacements := []*types.OrderPlacement{}
			for _, order := range args[1:] {
				orderPlacement := types.OrderPlacement{}
				orderDetails := strings.Split(order, ",")
				argPositionDir, err := types.GetPositionDirectionFromStr(orderDetails[0])
				if err != nil {
					return err
				}
				orderPlacement.PositionDirection = argPositionDir
				argPrice, err := sdk.NewDecFromStr(orderDetails[1])
				if err != nil {
					return err
				}
				orderPlacement.Price = argPrice
				argQuantity, err := sdk.NewDecFromStr(orderDetails[2])
				if err != nil {
					return err
				}
				orderPlacement.Quantity = argQuantity
				reqPriceDenom, err := types.GetDenomFromStr(orderDetails[3])
				if err != nil {
					return err
				}
				reqAssetDenom, err := types.GetDenomFromStr(orderDetails[4])
				if err != nil {
					return err
				}
				orderPlacement.PriceDenom = reqPriceDenom
				orderPlacement.AssetDenom = reqAssetDenom
				argPositionEffect, err := types.GetPositionEffectFromStr(orderDetails[5])
				if err != nil {
					return err
				}
				orderPlacement.PositionEffect = argPositionEffect
				argOrderType, err := types.GetOrderTypeFromStr(orderDetails[6])
				if err != nil {
					return err
				}
				orderPlacement.OrderType = argOrderType
				argLeverage, err := sdk.NewDecFromStr(orderDetails[7])
				if err != nil {
					return err
				}
				orderPlacement.Leverage = argLeverage
				orderPlacements = append(orderPlacements, &orderPlacement)
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			amountStr, err := cmd.Flags().GetString(flagAmount)
			if err != nil {
				return err
			}

			amount, err := sdk.ParseCoinsNormalized(amountStr)
			if err != nil {
				return err
			}

			msg := types.NewMsgPlaceOrders(
				clientCtx.GetFromAddress().String(),
				orderPlacements,
				argContractAddr,
				amount,
			)
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagAmount, "", "Coins to send to the contract along with command")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
