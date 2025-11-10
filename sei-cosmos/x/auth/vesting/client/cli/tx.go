package cli

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

// Transaction command flags
const (
	FlagDelayed = "delayed"
	FlagAdmin   = "admin"
)

// GetTxCmd returns vesting module's transaction commands.
func GetTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Vesting transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	txCmd.AddCommand(
		NewMsgCreateVestingAccountCmd(),
	)

	return txCmd
}

// NewMsgCreateVestingAccountCmd returns a CLI command handler for creating a
// MsgCreateVestingAccount transaction.
func NewMsgCreateVestingAccountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-vesting-account [to_address] [amount] [end_time]",
		Short: "Create a new vesting account funded with an allocation of tokens.",
		Long: `Create a new vesting account funded with an allocation of tokens. The
account can either be a delayed or continuous vesting account, which is determined
by the '--delayed' flag. All vesting accouts created will have their start time
set by the committed block's time. The end_time must be provided as a UNIX epoch
timestamp. You can also optionally configure the 'admin' field using the flag '--admin {addr}. This admin will be able to perform some administrative actions on the vesting account if set.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			toAddr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			amount, err := sdk.ParseCoinsNormalized(args[1])
			if err != nil {
				return err
			}

			endTime, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return err
			}

			delayed, _ := cmd.Flags().GetBool(FlagDelayed)

			var adminAddr sdk.AccAddress
			admin, _ := cmd.Flags().GetString(FlagAdmin)
			if len(admin) > 0 {
				adminAddr, err = sdk.AccAddressFromBech32(admin)
				if err != nil {
					return err
				}
			}

			msg := types.NewMsgCreateVestingAccount(clientCtx.GetFromAddress(), toAddr, amount, endTime, delayed, adminAddr)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().Bool(FlagDelayed, false, "Create a delayed vesting account if true")
	cmd.Flags().String(FlagAdmin, "", "Add an admin to the vesting account if set")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
