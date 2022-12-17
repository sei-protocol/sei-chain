package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/x/accesscontrol/client/utils"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/spf13/cobra"
)

func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	updateResourceDependencyMappingProposalCmd := MsgUpdateResourceDependencyMappingProposalCmd()
	flags.AddTxFlagsToCmd(updateResourceDependencyMappingProposalCmd)
	registerWasmDependencyMappingCmd := MsgRegisterWasmDependencyMappingCmd()
	flags.AddTxFlagsToCmd(registerWasmDependencyMappingCmd)

	cmd.AddCommand(
		updateResourceDependencyMappingProposalCmd,
		registerWasmDependencyMappingCmd,
	)

	return cmd
}

func MsgUpdateResourceDependencyMappingProposalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-resource-dependency-mapping [proposal-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Submit an UpdateResourceDependencyMapping proposal",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			proposal, err := utils.ParseMsgUpdateResourceDependencyMappingProposalFile(clientCtx.Codec, args[0])
			if err != nil {
				return err
			}

			from := clientCtx.GetFromAddress()

			content := types.MsgUpdateResourceDependencyMappingProposal{
				Title:                    proposal.Title,
				Description:              proposal.Description,
				MessageDependencyMapping: proposal.MessageDependencyMapping,
			}

			deposit, err := sdk.ParseCoinsNormalized(proposal.Deposit)
			if err != nil {
				return err
			}

			msg, err := govtypes.NewMsgSubmitProposal(&content, deposit, from)
			if err != nil {

				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	return cmd
}

func MsgRegisterWasmDependencyMappingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-wasm-dependency-mapping [mapping-json-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Register dependencies for a wasm contract",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			wasmDependencyJson, err := utils.ParseRegisterWasmDependencyMappingJSON(clientCtx.Codec, args[0])
			if err != nil {
				return err
			}
			from := clientCtx.GetFromAddress()

			msgWasmRegisterDependency := types.NewMsgRegisterWasmDependencyFromJSON(from, wasmDependencyJson)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgWasmRegisterDependency)
		},
	}

	return cmd
}
