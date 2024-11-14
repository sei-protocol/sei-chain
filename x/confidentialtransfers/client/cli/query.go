package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the cli query commands for the minting module.
func GetQueryCmd() *cobra.Command {
	confidentialTransfersQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the confidential transfer module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	confidentialTransfersQueryCmd.AddCommand(
		GetCmdQueryAccount(),
		GetCmdQueryAllAccount(),
	)

	return confidentialTransfersQueryCmd
}

// GetCmdQueryAccount implements a command to return an account asssociated with the address and denom
func GetCmdQueryAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account [address] [denom]",
		Short: "Query the account state",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.GetCtAccount(cmd.Context(), &types.GetCtAccountRequest{
				Address: args[0],
				Denom:   args[1],
			})

			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res.Account)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetCmdQueryAccount implements a command to return an account asssociated with the address and denom
func GetCmdQueryAllAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts [address]",
		Short: "Query all the confidential token accounts associated with the address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.GetAllCtAccounts(cmd.Context(), &types.GetAllCtAccountsRequest{
				Address: args[0],
			})

			if err != nil {
				return err
			}

			for i := range res.Accounts {
				err = clientCtx.PrintProto(&res.Accounts[i])
				if err != nil {
					return err
				}
			}

			return nil
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
