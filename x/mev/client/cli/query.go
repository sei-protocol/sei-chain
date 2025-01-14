package cli

import (
    "fmt"

    "github.com/spf13/cobra"
    
    "github.com/cosmos/cosmos-sdk/client"
    "github.com/cosmos/cosmos-sdk/client/flags"
    "github.com/sei-protocol/sei-chain/x/mev/types"
)

// GetQueryCmd returns the cli query commands for this module
func GetQueryCmd(queryRoute string) *cobra.Command {
    cmd := &cobra.Command{
        Use:                        types.ModuleName,
        Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
        DisableFlagParsing:         true,
        SuggestionsMinimumDistance: 2,
        RunE:                       client.ValidateCmd,
    }

    cmd.AddCommand(
        GetParamsCmd(),
    )

    return cmd
}

func GetParamsCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "params",
        Short: "Query the current mev module parameters",
        Args:  cobra.NoArgs,
        RunE: func(cmd *cobra.Command, args []string) error {
            clientCtx, err := client.GetClientQueryContext(cmd)
            if err != nil {
                return err
            }

            queryClient := types.NewQueryClient(clientCtx)
            res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
            if err != nil {
                return err
            }

            return clientCtx.PrintProto(&res.Params)
        },
    }

    flags.AddQueryFlagsToCmd(cmd)

    return cmd
}
