package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

// GetQueryCmd returns the query commands for the seinet module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s query commands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
	}

	cmd.AddCommand(NewVaultBalanceCmd())

	return cmd
}

// NewVaultBalanceCmd creates a CLI query to fetch the Seinet vault balance.
func NewVaultBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault-balance",
		Short: "Query the current balance of the Seinet vault module account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			resp, err := queryClient.VaultBalance(context.Background(), &types.QueryVaultBalanceRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
