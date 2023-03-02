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

func CmdGetOrders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-orders [contract-address] [account]",
		Short: "Query get orders for account",
		Long: strings.TrimSpace(`
			Get all orders for an account and orderbook specified by contract address.
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			reqContractAddr := args[0]
			reqAccount := args[1]

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetOrdersRequest{
				ContractAddr: reqContractAddr,
				Account:      reqAccount,
			}

			res, err := queryClient.GetOrders(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdGetOrdersByID() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-orders-by-id [contract-address] [price-denom] [asset-denom] [id]",
		Short: "Query get order by ID",
		Long: strings.TrimSpace(`
			Get a specific order by ID for an account and orderbook specified by contract address.
		`),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contractAddr := args[0]
			priceDenom := args[1]
			assetDenom := args[2]
			orderID, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return err
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetOrderByIDRequest{
				ContractAddr: contractAddr,
				PriceDenom:   priceDenom,
				AssetDenom:   assetDenom,
				Id:           orderID,
			}

			res, err := queryClient.GetOrder(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
