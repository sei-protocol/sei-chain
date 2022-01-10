package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"

	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/controller/types"
)

// GetCmdParams returns the command handler for the controller submodule parameter querying.
func GetCmdParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "params",
		Short:   "Query the current interchain-accounts controller submodule parameters",
		Long:    "Query the current interchain-accounts controller submodule parameters",
		Args:    cobra.NoArgs,
		Example: fmt.Sprintf("%s query interchain-accounts controller params", version.AppName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res.Params)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
