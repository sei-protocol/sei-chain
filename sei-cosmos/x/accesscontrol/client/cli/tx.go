package cli

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/accesscontrol/client/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/accesscontrol/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

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
		Long: "Submit a proposal to update resource dependencies between objects. \n" +
			"E.g. $ seid update-resource-dependency-mapping [proposal-file]\n" +
			"The proposal file should contain the following:\n" +
			"{\n" +
			"\t title: [title],\n" +
			"\t description: [description],\n" +
			"\t deposit: [deposit],\n" +
			"\t message_dependency_mapping: [<list of message dependency mappings>]\n" +
			"}",
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
		Long: "Registers dependencies for a wasm contract\n" +
			"E.g. $seid register-wasm-dependency-mapping [mapping-json-file]\n" +
			"The mapping JSON file should contain the following:\n" +
			"{\n" +
			"\t wasm_dependency_mapping: <wasm dependency mapping>\n" +
			"}",
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
