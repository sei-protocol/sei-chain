package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cosmos/go-bip39"
	"github.com/pkg/errors"
	"github.com/sei-protocol/sei-chain/app/genesis"
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/spf13/cobra"
	tmcfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/cli"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/client"
	clientconfig "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/input"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
)

const (
	// FlagOverwrite defines a flag to overwrite an existing genesis JSON file.
	FlagOverwrite = "overwrite"

	// FlagSeed defines a flag to initialize the private validator key from a specific seed.
	FlagRecover = "recover"

	// FlagMode defines the node mode flag.
	FlagMode = "mode"
)

// isValidMode checks if the given node mode is valid
func isValidMode(mode params.NodeMode) bool {
	switch mode {
	case params.NodeModeValidator, params.NodeModeFull, params.NodeModeSeed, params.NodeModeArchive:
		return true
	default:
		return false
	}
}

type printInfo struct {
	Moniker    string          `json:"moniker" yaml:"moniker"`
	ChainID    string          `json:"chain_id" yaml:"chain_id"`
	NodeID     string          `json:"node_id" yaml:"node_id"`
	GenTxsDir  string          `json:"gentxs_dir" yaml:"gentxs_dir"`
	AppMessage json.RawMessage `json:"app_message" yaml:"app_message"`
}

func newPrintInfo(moniker, chainID, nodeID, genTxsDir string, appMessage json.RawMessage) printInfo {
	return printInfo{
		Moniker:    moniker,
		ChainID:    chainID,
		NodeID:     nodeID,
		GenTxsDir:  genTxsDir,
		AppMessage: appMessage,
	}
}

func displayInfo(info printInfo) error {
	out, err := json.MarshalIndent(info, "", " ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(os.Stderr, "%s\n", string(sdk.MustSortJSON(out)))

	return err
}

// InitCmd returns a command that initializes all files needed for Tendermint
// and the respective application.
func InitCmd(mbm module.BasicManager, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [moniker]",
		Short: "Initialize private validator, p2p, genesis, and application configuration files",
		Long:  `Initialize validators's and node's configuration files.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			cdc := clientCtx.Codec

			// Get node mode from flag
			modeStr, _ := cmd.Flags().GetString(FlagMode)
			nodeMode := params.NodeMode(modeStr)

			// Validate mode
			if !isValidMode(nodeMode) {
				return fmt.Errorf("invalid node mode: %s (valid modes: validator, full, seed, archive)", modeStr)
			}

			// Create and configure Tendermint config (outputs to config.toml)
			tmConfig := tmcfg.DefaultConfig()
			// Tendermint only supports "validator", "full", "seed" modes
			// Archive nodes use "full" mode in Tendermint but have different app config
			if nodeMode == params.NodeModeArchive {
				tmConfig.Mode = string(params.NodeModeFull)
			} else {
				tmConfig.Mode = string(nodeMode)
			}
			params.SetTendermintConfigByMode(tmConfig)
			tmConfig.SetRoot(clientCtx.HomeDir)
			configPath := filepath.Join(tmConfig.RootDir, "config")

			chainID, _ := cmd.Flags().GetString(flags.FlagChainID)
			if chainID == "" {
				panic("chain-id is required, please set using --chain-id")
			}

			// Get bip39 mnemonic
			var mnemonic string
			recoverFlag, _ := cmd.Flags().GetBool(FlagRecover)
			if recoverFlag {
				inBuf := bufio.NewReader(cmd.InOrStdin())
				mnemonic, err := input.GetString("Enter your bip39 mnemonic", inBuf)
				if err != nil {
					return err
				}

				if !bip39.IsMnemonicValid(mnemonic) {
					return errors.New("invalid mnemonic")
				}
			}

			nodeID, _, err := genutil.InitializeNodeValidatorFilesFromMnemonic(tmConfig, mnemonic)
			if err != nil {
				return err
			}

			tmConfig.Moniker = args[0]

			genFile := tmConfig.GenesisFile()
			overwrite, _ := cmd.Flags().GetBool(FlagOverwrite)

			if !overwrite && tmos.FileExists(genFile) {
				return fmt.Errorf("genesis.json file already exists: %v", genFile)
			}

			var genDoc *types.GenesisDoc
			var appState json.RawMessage
			if genesis.IsWellKnown(chainID) {
				var err error
				genDoc, err = genesis.EmbeddedGenesisDoc(chainID)
				if err != nil {
					return errors.Wrapf(err, "failed to load embedded genesis for %s", chainID)
				}
				appState = genDoc.AppState
			} else {
				var err error
				appState, err = json.MarshalIndent(mbm.DefaultGenesis(cdc), "", " ")
				if err != nil {
					return errors.Wrap(err, "Failed to marshall default genesis state")
				}
				genDoc = &types.GenesisDoc{}
				if fi, err := os.Stat(genFile); err != nil {
					if !os.IsNotExist(err) {
						return err
					}
				} else {
					if fi.IsDir() {
						return fmt.Errorf("genesis path is a directory, not a file: %s", genFile)
					}
					genDoc, err = types.GenesisDocFromFile(genFile)
					if err != nil {
						return errors.Wrap(err, "Failed to read genesis doc from file")
					}
				}
				genDoc.ChainID = chainID
				genDoc.Validators = nil
				genDoc.AppState = appState
			}
			if err = genutil.ExportGenesisFile(genDoc, genFile); err != nil {
				return errors.Wrap(err, "Failed to export genesis file")
			}

			clientConfig, err := clientconfig.GetClientConfig(configPath, clientCtx.Viper)
			if err != nil {
				return err
			}
			if err = clientconfig.SetClientConfig(flags.FlagChainID, chainID, configPath, clientConfig); err != nil {
				return err
			}

			toPrint := newPrintInfo(tmConfig.Moniker, chainID, nodeID, "", appState)

			// Write Tendermint config.toml
			err = tmcfg.WriteConfigFile(tmConfig.RootDir, tmConfig)
			if err != nil {
				panic(err)
			}

			// Create and configure app config (outputs to app.toml)
			appConfig := srvconfig.DefaultConfig()
			params.SetAppConfigByMode(appConfig, nodeMode)

			// Configure EVM based on node mode
			evmConfig := evmrpcconfig.DefaultConfig
			params.SetEVMConfigByMode(&evmConfig, nodeMode)

			appTomlPath := filepath.Join(configPath, "app.toml")

			// Get custom template from root.go
			customAppTemplate, _ := initAppConfig()
			srvconfig.SetConfigTemplate(customAppTemplate)

			// Build custom app config with mode-specific values
			customAppConfig := NewCustomAppConfig(appConfig, evmConfig)

			srvconfig.WriteConfigFile(appTomlPath, customAppConfig)

			fmt.Fprintf(os.Stderr, "\nNode initialized with mode: %s\n", nodeMode)
			fmt.Fprintf(os.Stderr, "Configuration files generated in: %s\n\n", configPath)

			return displayInfo(toPrint)
		},
	}

	cmd.Flags().String(cli.HomeFlag, defaultNodeHome, "node's home directory")
	cmd.Flags().BoolP(FlagOverwrite, "o", false, "overwrite the genesis.json file")
	cmd.Flags().Bool(FlagRecover, false, "provide seed phrase to recover existing key instead of creating")
	cmd.Flags().String(flags.FlagChainID, "", "genesis file chain-id, if left blank will use sei")
	cmd.Flags().String(FlagMode, "full", "node mode: validator, full, seed, or archive")

	return cmd
}
