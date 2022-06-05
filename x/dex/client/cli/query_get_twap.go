package cli

import (
	"errors"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdGetTwap() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-twap [price-denom] [asset-denom]",
		Short: "Query getTwap",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			reqPriceDenom, unit, err := types.GetDenomFromStr(args[0])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}

			reqAssetDenom, unit, err := types.GetDenomFromStr(args[1])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetTwapRequest{
				PriceDenom: reqPriceDenom,
				AssetDenom: reqAssetDenom,
			}

			res, err := queryClient.GetTwap(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
