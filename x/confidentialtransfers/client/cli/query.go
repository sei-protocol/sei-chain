package cli

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/spf13/cobra"
)

const decryptAvailableBalanceFlag = "decrypt-available-balance"

// GetQueryCmd returns the cli query commands for the minting module.
func GetQueryCmd() *cobra.Command {
	confidentialTransfersQueryCmd := &cobra.Command{
		Use:                        types.ShortModuleName,
		Short:                      "Querying commands for the confidential transfer module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	confidentialTransfersQueryCmd.AddCommand(
		GetCmdQueryAccount(),
		GetCmdQueryAllAccount(),
		GetCmdQueryTx(),
	)

	return confidentialTransfersQueryCmd
}

// GetCmdQueryAccount implements a command to return an account asssociated with the address and denom
func GetCmdQueryAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account [denom] [address] [flags]",
		Short: "Query the account state",
		Long: "Queries the account state associated with the address and denom." +
			"Pass the --from flag to decrypt the account" +
			"Pass the --decrypt-available-balance flag to attempt to decrypt the available balance.",
		Args: cobra.ExactArgs(2),
		RunE: queryAccount,
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().String(flags.FlagFrom, "", "Name or address of private key to decrypt the account")
	cmd.Flags().Bool(decryptAvailableBalanceFlag, false, "Set this to attempt to decrypt the available balance")
	return cmd
}

func queryAccount(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(clientCtx)

	denom := args[0]

	// Validate denom
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

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
	if err != nil {
		return err
	}

	// If the --from flag passed matches the queried address, attempt to decrypt the contents
	if fromAddr.String() == address {
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

		decryptAvailableBalance, err := cmd.Flags().GetBool(decryptAvailableBalanceFlag)
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
}

// GetCmdQueryAccount implements a command to return an account asssociated with the address and denom
func GetCmdQueryAllAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts [address]",
		Short: "Query all the confidential token accounts associated with the address",
		Args:  cobra.ExactArgs(1),
		RunE:  queryAllAccounts,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func queryAllAccounts(cmd *cobra.Command, args []string) error {
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
		err = clientCtx.PrintString("\n")
		if err != nil {
			return err
		}

		err = clientCtx.PrintProto(&res.Accounts[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// GetCmdQueryTx implements a command to query a tx by it's transaction hash and return it in it's decrypted state.
func GetCmdQueryTx() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tx [hash]",
		Short: "Query the confidential transaction and decrypts it",
		Long: "Query the confidential transaction by it's tx hash and decrypts it using the private key of the account in --from" +
			"Pass the --from flag to decrypt the account" +
			"Pass the --decrypt-available-balance flag to attempt to decrypt the available balance. (This is an expensive operation)",
		Args: cobra.ExactArgs(1),
		RunE: queryDecryptedTx,
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().String(flags.FlagFrom, "", "Name or address of private key to decrypt the account")
	cmd.Flags().Bool(decryptAvailableBalanceFlag, false, "Set this to attempt to decrypt the available balance")
	return cmd
}

func queryDecryptedTx(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	txHashHex := args[0]

	from, err := cmd.Flags().GetString(flags.FlagFrom)
	if err != nil {
		return err
	}
	fromAddr, _, _, err := client.GetFromFields(clientCtx, clientCtx.Keyring, from)
	if err != nil {
		return err
	}

	decryptAvailableBalance, err := cmd.Flags().GetBool(decryptAvailableBalanceFlag)
	if err != nil {
		return err
	}

	// Decode the transaction hash from hex to bytes
	txHash, err := hex.DecodeString(txHashHex)
	if err != nil {
		return fmt.Errorf("failed to decode transaction hash: %w", err)
	}

	// Connect to the gRPC client
	node, err := clientCtx.GetNode()
	if err != nil {
		return fmt.Errorf("failed to connect to node: %w", err)
	}

	// Query the transaction using Tendermint's Tx endpoint
	res, err := node.Tx(context.Background(), txHash, false)
	if err != nil {
		return fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Decode the transaction
	var rawTx tx.Tx
	if err := clientCtx.Codec.Unmarshal(res.Tx, &rawTx); err != nil {
		return fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	decryptor := elgamal.NewTwistedElgamal()
	privateKey, err := getPrivateKey(cmd)

	if err != nil {
		return err
	}
	msgPrinted := false
	for _, msg := range rawTx.Body.Messages {
		result, foundMsg, err := handleDecryptableMessage(clientCtx.Codec, msg, decryptor, privateKey, decryptAvailableBalance, fromAddr.String())
		if !foundMsg {
			continue
		} else {
			if err != nil {
				return err
			}
			err = clientCtx.PrintProto(result)
			msgPrinted = true
			if err != nil {
				return err
			}
		}
	}

	if !msgPrinted {
		return fmt.Errorf("no decryptable message found in the transaction")
	}

	return nil
}

// Helper function to unmarshal a message and run its Decrypt() method
func handleDecryptableMessage(
	cdc codec.Codec,
	msgAny *cdctypes.Any,
	decryptor *elgamal.TwistedElGamal,
	privKey *ecdsa.PrivateKey,
	decryptAvailableBalance bool,
	address string) (msg proto.Message, foundDecryptableMsg bool, error error) {
	// Try to unmarshal the message as one of the known types
	var sdkmsg sdk.Msg
	err := cdc.UnpackAny(msgAny, &sdkmsg)
	if err != nil {
		return nil, false, nil
	}

	decryptable, ok := sdkmsg.(types.Decryptable)
	if !ok {
		return nil, false, nil
	}

	// Successfully unmarshaled as decryptable message, run the Decrypt() method
	result, err := decryptable.Decrypt(decryptor, *privKey, decryptAvailableBalance, address)
	if err != nil {
		return nil, true, fmt.Errorf("failed to decrypt message: %w", err)
	}

	return result, true, nil
}
