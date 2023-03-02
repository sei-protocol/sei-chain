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

func CmdGetAssetList() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-asset-list",
		Short: "Query Asset List",
		Long: strings.TrimSpace(`
			Returns the metadata for all assets. Dex asset metadata includes information such as IBC info (for IBC assets), the asset type, and standard token metadata from the bank module.
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryAssetListRequest{}

			res, err := queryClient.AssetList(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdGetAssetMetadata() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-asset-metadata [denom]",
		Short: "Query Asset Metadata",
		Long: strings.TrimSpace(`
			Returns the metadata for a specific asset based on the passed in denom. Dex asset metadata includes information such as IBC info (for IBC assets), the asset type, and standard token metadata from the bank module.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			denom := args[0]
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryAssetMetadataRequest{
				Denom: denom,
			}

			res, err := queryClient.AssetMetadata(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
