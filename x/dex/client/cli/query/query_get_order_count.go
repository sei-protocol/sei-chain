package query

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdGetOrderCount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-order-count [contract] [price denom] [asset denom] [LONG|SHORT] [price]",
		Short: "get number of orders at a price leve",
		Args:  cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			var direction types.PositionDirection
			switch args[3] {
			case "LONG":
				direction = types.PositionDirection_LONG
			case "SHORT":
				direction = types.PositionDirection_SHORT
			default:
				return fmt.Errorf("unknown direction %s", args[3])
			}
			price := sdk.MustNewDecFromStr(args[4])
			res, err := queryClient.GetOrderCount(context.Background(), &types.QueryGetOrderCountRequest{
				ContractAddr:      args[0],
				PriceDenom:        args[1],
				AssetDenom:        args[2],
				PositionDirection: direction,
				Price:             &price,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
