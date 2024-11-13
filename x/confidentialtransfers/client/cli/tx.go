package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
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

	return txCmd
}

// NewInitializeAccountTxCmd returns a CLI command handler for creating a MsgInitializeAccount transaction.
func NewInitializeAccountTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-account [denom] [flags]",
		Short: "Initialize confidential transfers account",
		Long:  `Initialize confidential transfers account for the specified denomination.`,
		Args:  cobra.ExactArgs(1),
		RunE:  makeInitializeAccountCmd,
	}

	cmd.Flags().String(FlagPrivateKey, "", "Private key of the account")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func makeInitializeAccountCmd(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	_, err = cmd.Flags().GetString(FlagPrivateKey)
	if err != nil {
		return err
	}
	// TODO: Get below values from NewInitializeAccount function once merged
	msg := &types.MsgInitializeAccount{
		FromAddress:        clientCtx.GetFromAddress().String(),
		Denom:              args[1],
		PublicKey:          nil,
		DecryptableBalance: "",
		Proofs:             nil,
	}
	if err = msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}
