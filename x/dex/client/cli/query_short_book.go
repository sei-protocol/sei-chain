package cli

import (
	"context"
	"errors"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdListShortBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-short-book [contract address] [price denom] [asset denom]",
		Short: "list all shortBook",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			reqPriceDenom, unit, err := types.GetDenomFromStr(args[1])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}
			reqAssetDenom, unit, err := types.GetDenomFromStr(args[2])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}

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
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			contractAddr := args[0]
			reqPriceDenom, unit, err := types.GetDenomFromStr(args[2])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}
			reqAssetDenom, unit, err := types.GetDenomFromStr(args[3])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}

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
