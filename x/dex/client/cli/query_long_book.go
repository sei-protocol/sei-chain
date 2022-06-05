package cli

import (
	"context"
	"errors"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdListLongBook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-long-book [contract address] [price denom] [asset denom]",
		Short: "list all longBook",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

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
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)
			contractAddr := args[0]
			price, err := sdk.NewDecFromStr(args[1])
			if err != nil {
				return err
			}
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

			params := &types.QueryGetLongBookRequest{
				Price:        price,
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
