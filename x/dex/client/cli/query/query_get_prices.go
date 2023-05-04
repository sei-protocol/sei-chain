package query

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdGetPrices() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-prices [contract-address] [price-denom] [asset-denom]",
		Short: "Query getPrices",
		Long: strings.TrimSpace(`
			Get all the prices for a pair from a dex specified by the contract-address. The price and asset denom are used to specify the dex pair for which to return the latest price. For the latest price use get-latest-price instead or for a specific timestamp use get-price.
		`),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			reqContractAddr := args[0]
			reqPriceDenom := args[1]
			reqAssetDenom := args[2]

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetPricesRequest{
				ContractAddr: reqContractAddr,
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
			}

			res, err := queryClient.GetPrices(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
