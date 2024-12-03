package cli

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/spf13/cobra"
)

const (
	FlagPrivateKey = "private-key"
)

// NewTxCmd returns a root CLI command handler for all x/confidentialtransfers transaction commands.
func NewTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:                        types.ShortModuleName,
		Short:                      "Confidential transfers transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	txCmd.AddCommand(NewInitializeAccountTxCmd())
	txCmd.AddCommand(NewCloseAccountTxCmd())
	txCmd.AddCommand(NewTransferTxCmd())
	txCmd.AddCommand(NewWithdrawTxCmd())
	txCmd.AddCommand(NewDepositTxCmd())
	txCmd.AddCommand(NewApplyPendingBalanceTxCmd())

	return txCmd
}

// NewInitializeAccountTxCmd returns a CLI command handler for creating a MsgInitializeAccount transaction.
func NewInitializeAccountTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-account [denom] [flags]",
		Short: "Initialize confidential transfers account",
		Long: `Initialize confidential transfers command creates account for the specified denomination and address 
        passed in --from flag.`,
		Args: cobra.ExactArgs(1),
		RunE: makeInitializeAccountCmd,
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeInitializeAccountCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	denom := args[0]
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	privKey, err := getPrivateKey(cmd, clientCtx.GetFromName())
	if err != nil {
		return err
	}
	initializeAccount, err := types.NewInitializeAccount(clientCtx.GetFromAddress().String(), denom, *privKey)
	if err != nil {
		return err
	}

	msg := types.NewMsgInitializeAccountProto(initializeAccount)

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

func getPrivateKey(cmd *cobra.Command, fromName string) (*ecdsa.PrivateKey, error) {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return nil, err
	}
	txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
	kb := txf.Keybase()
	info, err := kb.Key(fromName)
	if err != nil {
		return nil, err
	}
	localInfo, ok := info.(keyring.LocalInfo)
	if !ok {
		return nil, errors.New("can only associate address for local keys")
	}
	if localInfo.GetAlgo() != hd.Secp256k1Type {
		return nil, errors.New("can only use addresses using secp256k1")
	}
	priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
	if err != nil {
		return nil, err
	}
	privHex := hex.EncodeToString(priv.Bytes())
	key, _ := crypto.HexToECDSA(privHex)
	return key, nil
}

// NewCloseAccountTxCmd returns a CLI command handler for creating a MsgCloseAccount transaction.
func NewCloseAccountTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close-account [denom] [flags]",
		Short: "Close confidential transfers account",
		Long: `Close confidential transfers command closes (deletes) account for the specified denomination and address 
        passed in --from flag.`,
		Args: cobra.ExactArgs(1),
		RunE: makeCloseAccountCmd,
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeCloseAccountCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	denom := args[0]
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	queryClient := types.NewQueryClient(queryClientCtx)

	privKey, err := getPrivateKey(cmd, clientCtx.GetFromName())
	if err != nil {
		return err
	}

	account, err := getAccount(queryClient, clientCtx.GetFromAddress().String(), denom)
	if err != nil {
		return err
	}

	closeAccount, err := types.NewCloseAccount(
		*privKey,
		clientCtx.GetFromAddress().String(),
		denom,
		account.PendingBalanceLo,
		account.PendingBalanceHi,
		account.AvailableBalance)

	if err != nil {
		return err
	}

	msg := types.NewMsgCloseAccountProto(closeAccount)

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

// NewTransferTxCmd returns a CLI command handler for creating a MsgTransfer transaction.
func NewTransferTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer [denom] [to_address] [amount] [flags]",
		Short: "Make a confidential transfer to another address",
		Long: `Transfer command create a confidential transfer of the specified amount of the specified denomination to the specified address. 
        passed in. To add auditors to the transaction, pass the --auditors flag with a comma separated list of auditor addresses.`,
		Args: cobra.ExactArgs(3),
		RunE: makeTransferCmd,
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().StringSlice("auditors", []string{}, "List of auditors")

	return cmd
}

func makeTransferCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	queryClient := types.NewQueryClient(queryClientCtx)

	privKey, err := getPrivateKey(cmd, clientCtx.GetFromName())
	if err != nil {
		return err
	}

	fromAddress := clientCtx.GetFromAddress().String()
	denom := args[0]
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	toAddress := args[1]
	_, err = sdk.AccAddressFromBech32(toAddress)
	if err != nil {
		return fmt.Errorf("invalid address: %v", err)
	}

	amount, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return err
	}

	senderAccount, err := getAccount(queryClient, fromAddress, denom)
	if err != nil {
		return err
	}

	recipientAccount, err := getAccount(queryClient, toAddress, denom)
	if err != nil {
		return err
	}

	auditorAddrs, err := cmd.Flags().GetStringSlice("auditors")
	if err != nil {
		return err
	}

	auditors := make([]types.AuditorInput, len(auditorAddrs))
	for i, auditorAddr := range auditorAddrs {
		auditorAccount, err := getAccount(queryClient, auditorAddr, denom)
		if err != nil {
			return err
		}
		auditors[i] = types.AuditorInput{
			Address: auditorAddr,
			Pubkey:  &auditorAccount.PublicKey,
		}
	}

	transfer, err := types.NewTransfer(
		privKey,
		fromAddress,
		toAddress,
		denom,
		senderAccount.DecryptableAvailableBalance,
		senderAccount.AvailableBalance,
		amount,
		&recipientAccount.PublicKey,
		auditors)

	if err != nil {
		return err
	}

	msg := types.NewMsgTransferProto(transfer)

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

// NewWithdrawTxCmd returns a CLI command handler for creating a MsgWithdraw transaction.
func NewWithdrawTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw [denom] [amount] [flags]",
		Short: "Withdraw from confidential transfers account",
		Long: `Withdraws the specified amount from the confidential transfers account for the specified denomination and address 
        passed in --from flag.`,
		Args: cobra.ExactArgs(2),
		RunE: makeWithdrawCmd,
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeWithdrawCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	queryClient := types.NewQueryClient(queryClientCtx)

	privKey, err := getPrivateKey(cmd, clientCtx.GetFromName())
	if err != nil {
		return err
	}
	address := clientCtx.GetFromAddress().String()

	denom := args[0]
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	amount, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return err
	}

	account, err := getAccount(queryClient, address, denom)
	if err != nil {
		return err
	}

	withdraw, err := types.NewWithdraw(
		*privKey,
		account.AvailableBalance,
		denom,
		address,
		account.DecryptableAvailableBalance,
		amount)

	if err != nil {
		return err
	}

	msg := types.NewMsgWithdrawProto(withdraw)

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

// NewDepositTxCmd returns a CLI command handler for creating a MsgDeposit transaction.
func NewDepositTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit [denom] [amount] [flags]",
		Short: "Deposit funds into confidential transfers account",
		Long: `Deposit the specified amount into the confidential transfers account for the specified denomination and address 
        passed in --from flag.`,
		Args: cobra.ExactArgs(2),
		RunE: makeDepositCmd,
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeDepositCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	address := clientCtx.GetFromAddress().String()
	denom := args[0]
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	amount, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return err
	}

	msg := &types.MsgDeposit{
		FromAddress: address,
		Denom:       denom,
		Amount:      amount,
	}

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

// NewApplyPendingBalanceCmd returns a CLI command handler for creating a MsgDeposit transaction.
func NewApplyPendingBalanceTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply-pending-balance [denom] [flags]",
		Short: "Applies the pending balances to the available balances",
		Long: `Makes the pending balances of the confidential token account for the specified denomination and address 
        passed in --from flag spendable by moving them to the available balance.`,
		Args: cobra.ExactArgs(1),
		RunE: makeApplyPendingBalanceCmd,
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeApplyPendingBalanceCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	queryClient := types.NewQueryClient(queryClientCtx)
	privKey, err := getPrivateKey(cmd, clientCtx.GetFromName())
	if err != nil {
		return err
	}

	address := clientCtx.GetFromAddress().String()
	denom := args[0]
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	account, err := getAccount(queryClient, clientCtx.GetFromAddress().String(), denom)
	if err != nil {
		return err
	}

	applyPendingBalance, err := types.NewApplyPendingBalance(
		*privKey,
		address,
		denom,
		account.DecryptableAvailableBalance,
		account.PendingBalanceCreditCounter,
		account.AvailableBalance,
		account.PendingBalanceLo,
		account.PendingBalanceHi)

	if err != nil {
		return err
	}

	msg := types.NewMsgApplyPendingBalanceProto(applyPendingBalance)

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

// Uses the query client to get the account data for the given address and denomination.
func getAccount(queryClient types.QueryClient, address, denom string) (*types.Account, error) {
	ctAccount, err := queryClient.GetCtAccount(context.Background(), &types.GetCtAccountRequest{
		Address: address,
		Denom:   denom,
	})
	if err != nil {
		return nil, err
	}

	account, err := ctAccount.GetAccount().FromProto()
	if err != nil {
		return nil, err
	}

	return account, nil
}
