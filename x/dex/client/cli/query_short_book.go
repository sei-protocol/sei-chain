package cli

import (
	"context"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdListShortBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-short-book",
		Short: "list all shortBook",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryAllShortBookRequest{
				Pagination: pageReq,
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
		Use:   "show-short-book [contract address] [id] [price denom] [asset denom]",
		Short: "shows a shortBook",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			contractAddr := args[0]
			id, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return err
			}
			priceDenom := args[2]
			assetDenom := args[3]

			params := &types.QueryGetShortBookRequest{
				Id:           id,
				ContractAddr: contractAddr,
				PriceDenom:   priceDenom,
				AssetDenom:   assetDenom,
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
