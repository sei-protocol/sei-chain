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
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/spf13/cobra"

	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	clientconfig "github.com/sei-protocol/sei-chain/sei-cosmos/client/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/input"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
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
		Long: `Initialize the node's configuration files. Default mode is "full" (RPC and P2P bind to all interfaces).
For validator or seed nodes, pass --mode validator or --mode seed so RPC and P2P bind to localhost only.`,
		Args: cobra.ExactArgs(1),
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
			serverCtx := server.GetServerContextFromCmd(cmd)
			genDoc, err := loadOrWriteGenesis(serverCtx.Logger, genFile, chainID, overwrite, mbm, cdc)
			if err != nil {
				return err
			}

			clientConfig, err := clientconfig.GetClientConfig(configPath, clientCtx.Viper)
			if err != nil {
				return err
			}
			if err = clientconfig.SetClientConfig(flags.FlagChainID, chainID, configPath, clientConfig); err != nil {
				return err
			}

			toPrint := newPrintInfo(tmConfig.Moniker, chainID, nodeID, "", genDoc.AppState)

			if err := checkConfigOverwrite(configPath, overwrite); err != nil {
				return err
			}

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

			// Get custom template from root.go
			customAppTemplate, _ := initAppConfig()
			srvconfig.SetConfigTemplate(customAppTemplate)

			// Build custom app config with mode-specific values
			customAppConfig := NewCustomAppConfig(appConfig, evmConfig)

			appTomlPath := filepath.Join(configPath, "app.toml")
			srvconfig.WriteConfigFile(appTomlPath, customAppConfig)

			fmt.Fprintf(os.Stderr, "\nNode initialized with mode: %s\n", nodeMode)
			fmt.Fprintf(os.Stderr, "Configuration files generated in: %s\n\n", configPath)

			return displayInfo(toPrint)
		},
	}

	cmd.Flags().String(cli.HomeFlag, defaultNodeHome, "node's home directory")
	cmd.Flags().BoolP(FlagOverwrite, "o", false, "overwrite the genesis.json and existing config files (config.toml, app.toml)")
	cmd.Flags().Bool(FlagRecover, false, "provide seed phrase to recover existing key instead of creating")
	cmd.Flags().String(flags.FlagChainID, "", "genesis file chain-id, if left blank will use sei")
	cmd.Flags().String(FlagMode, "full", "node mode: validator, full, seed, or archive")

	return cmd
}

func checkConfigOverwrite(configPath string, overwrite bool) error {
	if overwrite {
		return nil
	}
	configTomlPath := filepath.Join(configPath, "config.toml")
	appTomlPath := filepath.Join(configPath, "app.toml")
	if tmos.FileExists(configTomlPath) || tmos.FileExists(appTomlPath) {
		return fmt.Errorf("configuration files already exist in %s; if you intend to override them, use the --overwrite flag", configPath)
	}
	return nil
}

// loadOrWriteGenesis loads existing genesis at genFile if present and !overwrite, else writes embedded (well-known) or default.
func loadOrWriteGenesis(logger log.Logger, genFile, chainID string, overwrite bool, mbm module.BasicManager, cdc codec.JSONCodec) (*types.GenesisDoc, error) {
	if !overwrite && tmos.FileExists(genFile) {
		if err := ensureGenesisPathIsFile(genFile); err != nil {
			return nil, err
		}
		genDoc, err := types.GenesisDocFromFile(genFile)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read genesis file %s", genFile)
		}
		if genDoc.ChainID != chainID {
			return nil, fmt.Errorf("genesis file %s has chain_id %q but init chain-id is %q", genFile, genDoc.ChainID, chainID)
		}
		if logger != nil {
			logger.Debug("using existing genesis file (not overwriting)", "path", genFile)
		}
		return genDoc, nil
	}

	if genesis.IsWellKnown(chainID) {
		genDoc, err := genesis.EmbeddedGenesisDoc(chainID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load embedded genesis for %s", chainID)
		}
		if logger != nil {
			logger.Debug("writing genesis file (embedded)", "chain_id", chainID, "path", genFile)
		}
		if err := genutil.ExportGenesisFile(genDoc, genFile); err != nil {
			return nil, errors.Wrap(err, "Failed to export genesis file")
		}
		return genDoc, nil
	}

	appState, err := json.MarshalIndent(mbm.DefaultGenesis(cdc), "", " ")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to marshall default genesis state")
	}
	genDoc := &types.GenesisDoc{ChainID: chainID, AppState: appState}
	if tmos.FileExists(genFile) {
		if err := ensureGenesisPathIsFile(genFile); err != nil {
			return nil, err
		}
		var err error
		genDoc, err = types.GenesisDocFromFile(genFile)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to read genesis doc from file")
		}
		genDoc.ChainID = chainID
		genDoc.Validators = nil
		genDoc.AppState = appState
	}

	if logger != nil {
		logger.Debug("writing genesis file (default)", "chain_id", chainID, "path", genFile)
	}
	if err := genutil.ExportGenesisFile(genDoc, genFile); err != nil {
		return nil, errors.Wrap(err, "Failed to export genesis file")
	}
	return genDoc, nil
}

func ensureGenesisPathIsFile(genFile string) error {
	fi, err := os.Stat(genFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrapf(err, "failed to stat genesis file %s", genFile)
	}
	if fi.IsDir() {
		return fmt.Errorf("genesis path is a directory, not a file: %s", genFile)
	}
	return nil
}
