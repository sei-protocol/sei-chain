package cli

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdListSettlements() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-settlements",
		Short: "get settlements",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			contractAddr := args[0]
			priceDenom := args[1]
			assetDenom := args[2]
			orderID, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return err
			}
			query := &types.QueryGetSettlementsRequest{
				ContractAddr: contractAddr,
				PriceDenom:   priceDenom,
				AssetDenom:   assetDenom,
				OrderId:      orderID,
			}

			res, err := queryClient.GetSettlements(cmd.Context(), query)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
