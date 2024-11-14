package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
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
		Use:   "account [denom] [address] [flags]",
		Short: "Query the account state",
		Long: "Queries the account state associated with the address and denom." +
			"Pass the --from flag to decrypt the account." +
			"Pass the --decryptAvailableBalance flag to attempt to decrypt the available balance.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			denom := args[0]
			address := args[1]
			// Validate address
			_, err = sdk.AccAddressFromBech32(address)
			if err != nil {
				return fmt.Errorf("invalid address: %v", err)
			}

			from, err := cmd.Flags().GetString(flags.FlagFrom)
			if err != nil {
				return err
			}
			fromAddr, _, _, err := client.GetFromFields(clientCtx, clientCtx.Keyring, from)
			if err != nil {
				return err
			}

			res, err := queryClient.GetCtAccount(cmd.Context(), &types.GetCtAccountRequest{
				Address: address,
				Denom:   denom,
			})

			if fromAddr.String() == args[0] {
				account, err := res.Account.FromProto()
				if err != nil {
					return err
				}
				privateKey, err := getPrivateKey(cmd)
				if err != nil {
					return err
				}

				aesKey, err := encryption.GetAESKey(*privateKey, denom)
				if err != nil {
					return err
				}

				decryptor := elgamal.NewTwistedElgamal()
				keyPair, err := decryptor.KeyGen(*privateKey, denom)
				if err != nil {
					return err
				}

				decryptAvailableBalance, err := cmd.Flags().GetBool("decryptAvailableBalance")
				if err != nil {
					return err
				}

				decryptedAccount, err := account.Decrypt(decryptor, keyPair, aesKey, decryptAvailableBalance)
				if err != nil {
					return err
				}

				return clientCtx.PrintProto(decryptedAccount)

			} else {
				return clientCtx.PrintProto(res.Account)
			}
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().String(flags.FlagFrom, "", "Name or address of private key to decrypt the account")
	cmd.Flags().Bool("decryptAvailableBalance", false, "Set this to attempt to decrypt the available balance")
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

			address := args[0]
			// Validate address
			_, err = sdk.AccAddressFromBech32(address)
			if err != nil {
				return fmt.Errorf("invalid address: %v", err)
			}

			res, err := queryClient.GetAllCtAccounts(cmd.Context(), &types.GetAllCtAccountsRequest{
				Address: args[0],
			})

			if err != nil {
				return err
			}

			for i := range res.Accounts {
				clientCtx.PrintString("\n")
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
