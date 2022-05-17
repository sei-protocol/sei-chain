package cli

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

const (
	flagAmount = "amount"
)

func CmdPlaceOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "place-orders [contract address] [nonce] [orders...] --amount [coins,optional]",
		Short: "Bulk place orders",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			argNonce, err := cast.ToUint64E(args[1])
			if err != nil {
				return err
			}
			orderPlacements := []*types.OrderPlacement{}
			for _, order := range args[2:] {
				orderPlacement := types.OrderPlacement{}
				orderDetails := strings.Split(order, ",")
				orderPlacement.Long = orderDetails[0] == "Long"
				argPrice, err := cast.ToUint64E(orderDetails[1])
				if err != nil {
					return err
				}
				orderPlacement.Price = argPrice
				argQuantity, err := cast.ToUint64E(orderDetails[2])
				if err != nil {
					return err
				}
				orderPlacement.Quantity = argQuantity
				orderPlacement.PriceDenom = orderDetails[3]
				orderPlacement.AssetDenom = orderDetails[4]
				orderPlacement.Open = orderDetails[5] == "Open"
				orderPlacement.Limit = orderDetails[6] == "Limit"
				orderPlacement.Leverage = orderDetails[7]
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
				argNonce,
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
