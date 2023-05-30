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

func CmdGetPrice() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-price [contract-address] [timestamp] [price-denom] [asset-denom]",
		Short: "Query getPrice",
		Long: strings.TrimSpace(`
			Get the price for a pair from a dex specified by the contract-address. The price and asset denom are used to specify the dex pair for which to return the latest price. The timestamp is used to query the price for that specific timestamp.  For the latest price use get-latest-price instead or for all prices use get-prices.
		`),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			reqContractAddr := args[0]
			reqTimestamp, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return err
			}
			reqPriceDenom := args[2]
			reqAssetDenom := args[3]

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetPriceRequest{
				ContractAddr: reqContractAddr,
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
				Timestamp:    reqTimestamp,
			}

			res, err := queryClient.GetPrice(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
