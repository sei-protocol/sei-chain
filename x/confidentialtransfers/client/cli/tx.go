package cli

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
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

	privKey, err := getPrivateKey(cmd)
	if err != nil {
		return err
	}
	initializeAccount, err := types.NewInitializeAccount(clientCtx.GetFromAddress().String(), args[0], *privKey)
	if err != nil {
		return err
	}

	msg := types.NewMsgInitializeAccountProto(initializeAccount)

	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}

func getPrivateKey(cmd *cobra.Command) (*ecdsa.PrivateKey, error) {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return nil, err
	}
	txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
	kb := txf.Keybase()
	info, err := kb.Key(clientCtx.GetFromName())
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

	cmd.Flags().String(FlagPrivateKey, "", "Private key of the account")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeCloseAccountCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	_, err = cmd.Flags().GetString(FlagPrivateKey)
	if err != nil {
		return err
	}
	// TODO: Get below values from NewCloseAccount function once merged
	msg := &types.MsgCloseAccount{
		Address: clientCtx.GetFromAddress().String(),
		Denom:   args[1],
		Proofs:  nil,
	}
	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}
