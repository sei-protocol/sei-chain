package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"

	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

// GetTxCmd wires accesscontrol transaction sub-commands into the root tx tree.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "accesscontrol",
		Short:                      "Access control transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewRegisterWasmDependencyCmd())

	return cmd
}

// NewRegisterWasmDependencyCmd builds a CLI command for MsgRegisterWasmDependency.
func NewRegisterWasmDependencyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-wasm-dependency [mapping-json-or-path]",
		Short: "Register or update the wasm dependency mapping for a contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			mapping, err := parseWasmDependencyMapping(args[0])
			if err != nil {
				return err
			}

			msg := &acltypes.MsgRegisterWasmDependency{
				FromAddress:           clientCtx.GetFromAddress().String(),
				WasmDependencyMapping: mapping,
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseWasmDependencyMapping(input string) (*acltypes.WasmDependencyMapping, error) {
	raw := []byte(input)
	if info, err := os.Stat(input); err == nil && !info.IsDir() {
		fileContents, err := os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("failed to read wasm dependency mapping file: %w", err)
		}
		raw = fileContents
	}

	var mapping acltypes.WasmDependencyMapping
	if err := json.Unmarshal(raw, &mapping); err != nil {
		return nil, fmt.Errorf("failed to decode wasm dependency mapping JSON: %w", err)
	}

	if mapping.ContractAddress == "" {
		return nil, fmt.Errorf("contract_address is required in wasm dependency mapping")
	}

	return &mapping, nil
}
