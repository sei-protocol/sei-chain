package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

const flagAddress = "address"

// GetQueryCmd returns the cli query commands for this module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewVaultBalanceCmd(),
		NewCovenantBalanceCmd(),
	)

	return cmd
}

// NewVaultBalanceCmd creates a command to query the seinet vault module account balance.
func NewVaultBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault-balance",
		Short: "Query the balance of the seinet vault module account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			address, err := cmd.Flags().GetString(flagAddress)
			if err != nil {
				return err
			}
			if address != "" {
				if _, err := sdk.AccAddressFromBech32(address); err != nil {
					return err
				}
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.VaultBalance(cmd.Context(), &types.QueryVaultBalanceRequest{Address: address})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String(flagAddress, "", "optional bech32 address to query instead of the default module account")
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// NewCovenantBalanceCmd creates a command to query the seinet covenant module account balance.
func NewCovenantBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "covenant-balance",
		Short: "Query the balance of the seinet covenant module account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			address, err := cmd.Flags().GetString(flagAddress)
			if err != nil {
				return err
			}
			if address != "" {
				if _, err := sdk.AccAddressFromBech32(address); err != nil {
					return err
				}
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.CovenantBalance(cmd.Context(), &types.QueryCovenantBalanceRequest{Address: address})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String(flagAddress, "", "optional bech32 address to query instead of the default module account")
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
