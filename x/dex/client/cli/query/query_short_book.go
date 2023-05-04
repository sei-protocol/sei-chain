package query

import (
	"context"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdListShortBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-short-book [contract address] [price denom] [asset denom]",
		Short: "list all shortBook",
		Long: strings.TrimSpace(`
			Lists all of a short book's information for a given contract address and pair specified by price denopm and asset denom.
		`),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			reqPriceDenom := args[1]
			reqAssetDenom := args[2]

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryAllShortBookRequest{
				Pagination:   pageReq,
				ContractAddr: args[0],
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
			}

			res, err := queryClient.ShortBookAll(context.Background(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, cmd.Use)
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdShowShortBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-short-book [contract address] [price] [price denom] [asset denom]",
		Short: "shows a shortBook",
		Long: strings.TrimSpace(`
			Gets a short book's information at a specific price for a given contract address and pair specified by price denopm and asset denom.
		`),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			contractAddr := args[0]
			reqPriceDenom := args[2]
			reqAssetDenom := args[3]

			params := &types.QueryGetShortBookRequest{
				Price:        args[1],
				ContractAddr: contractAddr,
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
			}

			res, err := queryClient.ShortBook(context.Background(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
