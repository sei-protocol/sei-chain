package query

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdGetLatestPrice() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-latest-price [contract-address] [price-denom] [asset-denom]",
		Short: "Query getPrice",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			reqContractAddr := args[0]
			reqPriceDenom := args[1]
			reqAssetDenom := args[2]

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetLatestPriceRequest{
				ContractAddr: reqContractAddr,
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
			}

			res, err := queryClient.GetLatestPrice(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
