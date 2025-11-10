package cli

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/spf13/cobra"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// MigrateContractCmd will migrate a contract to a new code version
func MigrateContractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "migrate [contract_addr_bech32] [new_code_id_int64] [json_encoded_migration_args]",
		Short:   "Migrate a wasm contract to a new code version",
		Aliases: []string{"update", "mig", "m"},
		Args:    cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg, err := parseMigrateContractArgs(args, clientCtx)
			if err != nil {
				return err
			}
			if err := msg.ValidateBasic(); err != nil {
				return nil
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseMigrateContractArgs(args []string, cliCtx client.Context) (types.MsgMigrateContract, error) {
	// get the id of the code to instantiate
	codeID, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return types.MsgMigrateContract{}, sdkerrors.Wrap(err, "code id")
	}

	migrateMsg := args[2]

	msg := types.MsgMigrateContract{
		Sender:   cliCtx.GetFromAddress().String(),
		Contract: args[0],
		CodeID:   codeID,
		Msg:      []byte(migrateMsg),
	}
	return msg, nil
}

// UpdateContractAdminCmd sets an new admin for a contract
func UpdateContractAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "set-contract-admin [contract_addr_bech32] [new_admin_addr_bech32]",
		Short:   "Set new admin for a contract",
		Aliases: []string{"new-admin", "admin", "set-adm", "sa"},
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg, err := parseUpdateContractAdminArgs(args, clientCtx)
			if err != nil {
				return err
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseUpdateContractAdminArgs(args []string, cliCtx client.Context) (types.MsgUpdateAdmin, error) {
	msg := types.MsgUpdateAdmin{
		Sender:   cliCtx.GetFromAddress().String(),
		Contract: args[0],
		NewAdmin: args[1],
	}
	return msg, nil
}

// ClearContractAdminCmd clears an admin for a contract
func ClearContractAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "clear-contract-admin [contract_addr_bech32]",
		Short:   "Clears admin for a contract to prevent further migrations",
		Aliases: []string{"clear-admin", "clr-adm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.MsgClearAdmin{
				Sender:   clientCtx.GetFromAddress().String(),
				Contract: args[0],
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
