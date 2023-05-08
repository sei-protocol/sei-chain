package tx

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

func CmdCancelOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel-orders [contract address] [cancellations...]",
		Short: "Bulk cancel orders",
		Long: strings.TrimSpace(`
			Cancel orders placed on an orderbook specified by contract-address. Cancellations are represented as strings with the cancellation details separated by "?". Cancellation details format is OrderID?PositionDirection?Price?PriceDenom?AssetDenom.

			Example: "1234?LONG?1.01?USDC?ATOM"
		`),
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			if err != nil {
				return err
			}
			cancellations := []*types.Cancellation{}
			for _, cancellation := range args[1:] {
				newCancel := types.Cancellation{}
				cancelDetails := strings.Split(cancellation, "?")
				newCancel.Id, err = strconv.ParseUint(cancelDetails[0], 10, 64)
				if err != nil {
					return err
				}
				argPositionDir, err := types.GetPositionDirectionFromStr(cancelDetails[1])
				if err != nil {
					return err
				}
				newCancel.PositionDirection = argPositionDir
				argPrice, err := sdk.NewDecFromStr(cancelDetails[2])
				if err != nil {
					return err
				}
				newCancel.Price = argPrice
				newCancel.PriceDenom = cancelDetails[3]
				newCancel.AssetDenom = cancelDetails[4]
				cancellations = append(cancellations, &newCancel)
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgCancelOrders(
				clientCtx.GetFromAddress().String(),
				cancellations,
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
