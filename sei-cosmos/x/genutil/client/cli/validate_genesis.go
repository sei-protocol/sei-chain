package cli

import (
	"encoding/json"
	"fmt"

	genesistypes "github.com/cosmos/cosmos-sdk/types/genesis"
	"github.com/spf13/cobra"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/types/module"
)

const (
	chainUpgradeGuide = "https://docs.cosmos.network/master/migrations/chain-upgrade-guide-040.html"
	flagStreaming     = "streaming"
)

// ValidateGenesisCmd takes a genesis file, and makes sure that it is valid.
func ValidateGenesisCmd(mbm module.BasicManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate-genesis [file]",
		Args:  cobra.RangeArgs(0, 1),
		Short: "validates the genesis file at the default location or at the location passed as an arg",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			serverCtx := server.GetServerContextFromCmd(cmd)
			clientCtx := client.GetClientContextFromCmd(cmd)

			cdc := clientCtx.Codec

			isStream, err := cmd.Flags().GetBool(flagStreaming)
			if err != nil {
				panic(err)
			}

			if isStream {
				return validateGenesisStream(mbm, cmd, args)
			}

			// Load default if passed no args, otherwise load passed file
			var genesis string
			if len(args) == 0 {
				genesis = serverCtx.Config.GenesisFile()
			} else {
				genesis = args[0]
			}

			genDoc, err := validateGenDoc(genesis)
			if err != nil {
				return err
			}

			var genState map[string]json.RawMessage
			if err = json.Unmarshal(genDoc.AppState, &genState); err != nil {
				return fmt.Errorf("error unmarshalling genesis doc %s: %s", genesis, err.Error())
			}

			if err = mbm.ValidateGenesis(cdc, clientCtx.TxConfig, genState); err != nil {
				return fmt.Errorf("error validating genesis file %s: %s", genesis, err.Error())
			}

			fmt.Printf("File at %s is a valid genesis file\n", genesis)
			return nil
		},
	}
	cmd.Flags().Bool(flagStreaming, false, "turn on streaming mode with this flag")
	return cmd
}

type AppState struct {
	Module string          `json:"module"`
	Data   json.RawMessage `json:"data"`
}

type ModuleState struct {
	AppState AppState `json:"app_state"`
}

func parseModule(jsonStr string) (*ModuleState, error) {
	var module ModuleState
	err := json.Unmarshal([]byte(jsonStr), &module)
	if err != nil {
		return nil, err
	}
	if module.AppState.Module == "" {
		return nil, fmt.Errorf("module name is empty")
	}
	return &module, nil
}

func validateGenesisStream(mbm module.BasicManager, cmd *cobra.Command, args []string) error {
	serverCtx := server.GetServerContextFromCmd(cmd)
	clientCtx := client.GetClientContextFromCmd(cmd)

	cdc := clientCtx.Codec

	// Load default if passed no args, otherwise load passed file
	var genesis string
	if len(args) == 0 {
		genesis = serverCtx.Config.GenesisFile()
	} else {
		genesis = args[0]
	}

	lines := genesistypes.IngestGenesisFileLineByLine(genesis)

	genesisCh := make(chan json.RawMessage)
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	seenModules := make(map[string]bool)
	prevModule := ""
	var moduleName string
	var genDoc *tmtypes.GenesisDoc
	go func() {
		for line := range lines {
			moduleState, err := parseModule(line)
			// determine module name or genesisDoc
			if err != nil {
				genDoc, err = tmtypes.GenesisDocFromJSON([]byte(line))
				if err != nil {
					errCh <- fmt.Errorf("error unmarshalling genesis doc %s: %s", genesis, err.Error())
					return
				}
				moduleName = "genesisDoc"
			} else {
				moduleName = moduleState.AppState.Module
			}
			if seenModules[moduleName] {
				errCh <- fmt.Errorf("module %s seen twice in genesis file", moduleName)
				return
			}
			if prevModule != moduleName { // new module
				if prevModule != "" && prevModule != "genesisDoc" {
					doneCh <- struct{}{}
				}
				seenModules[prevModule] = true
				if moduleName != "genesisDoc" {
					go mbm.ValidateGenesisStream(cdc, clientCtx.TxConfig, moduleName, genesisCh, doneCh, errCh)
					genesisCh <- moduleState.AppState.Data
				} else {
					err = genDoc.ValidateAndComplete()
					if err != nil {
						errCh <- fmt.Errorf("error validating genesis doc %s: %s", genesis, err.Error())
					}
				}
			} else { // same module
				genesisCh <- moduleState.AppState.Data
			}
			prevModule = moduleName
		}
		fmt.Printf("File at %s is a valid genesis file\n", genesis)
		errCh <- nil
	}()
	err := <-errCh
	return err
}

// validateGenDoc reads a genesis file and validates that it is a correct
// Tendermint GenesisDoc. This function does not do any cosmos-related
// validation.
func validateGenDoc(importGenesisFile string) (*tmtypes.GenesisDoc, error) {
	genDoc, err := tmtypes.GenesisDocFromFile(importGenesisFile)
	if err != nil {
		return nil, fmt.Errorf("%s. Make sure that"+
			" you have correctly migrated all Tendermint consensus params, please see the"+
			" chain migration guide at %s for more info",
			err.Error(), chainUpgradeGuide,
		)
	}

	return genDoc, nil
}
