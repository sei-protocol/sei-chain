package query

import (
	"context"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdListLongBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-long-book [contract address] [price denom] [asset denom]",
		Short: "list all longBook",
		Long: strings.TrimSpace(`
			Lists all of a long book's information for a given contract address and pair specified by price denom and asset denom.
		`),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			reqPriceDenom := args[1]
			reqAssetDenom := args[2]

			params := &types.QueryAllLongBookRequest{
				Pagination:   pageReq,
				ContractAddr: args[0],
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
			}

			res, err := queryClient.LongBookAll(context.Background(), params)
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

func CmdShowLongBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-long-book [contract address] [price] [price denom] [asset denom]",
		Short: "shows a longBook",
		Long: strings.TrimSpace(`
			Gets a long book's information at a specific price for a given contract address and pair specified by price denopm and asset denom.
		`),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)
			contractAddr := args[0]
			reqPriceDenom := args[2]
			reqAssetDenom := args[3]

			params := &types.QueryGetLongBookRequest{
				Price:        args[1],
				ContractAddr: contractAddr,
				PriceDenom:   reqPriceDenom,
				AssetDenom:   reqAssetDenom,
			}
			res, err := queryClient.LongBook(context.Background(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
