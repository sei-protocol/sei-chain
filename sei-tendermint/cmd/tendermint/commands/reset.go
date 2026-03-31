package commands

import (
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/spf13/cobra"
)

const wasmDirName = "wasm"

// MakeResetCommand constructs a command that removes the database of
// the specified Tendermint core instance.
func MakeResetCommand(conf *config.Config) *cobra.Command {
	var keyType string

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Set of commands to conveniently reset tendermint related data",
	}

	resetBlocksCmd := &cobra.Command{
		Use:   "blockchain",
		Short: "Removes all blocks, state, transactions and evidence stored by the tendermint node",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ResetState(conf.DBDir())
		},
	}

	resetPeersCmd := &cobra.Command{
		Use:   "peers",
		Short: "Removes all peer addresses",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ResetPeerStore(conf.DBDir())
		},
	}

	resetSignerCmd := &cobra.Command{
		Use:   "unsafe-signer",
		Short: "esets private validator signer state",
		Long: `Resets private validator signer state.
Only use in testing. This can cause the node to double sign`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ResetFilePV(conf.PrivValidator.KeyFile(), conf.PrivValidator.StateFile(), keyType)
		},
	}

	resetAllCmd := &cobra.Command{
		Use:   "unsafe-all",
		Short: "Removes all tendermint data including signing state",
		Long: `Removes all tendermint data including signing state.
Only use in testing. This can cause the node to double sign`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := cmd.Flags().GetString(cli.HomeFlag)
			if err != nil {
				return err
			}
			// If home is empty, use conf.RootDir as a fallback
			if home == "" {
				home = conf.RootDir
			}
			return ResetAll(conf.DBDir(), conf.PrivValidator.KeyFile(),
				conf.PrivValidator.StateFile(), keyType, home)
		},
	}

	resetSignerCmd.Flags().StringVar(&keyType, "key", types.ABCIPubKeyTypeEd25519,
		"Signer key type (ed25519 only)")

	resetAllCmd.Flags().StringVar(&keyType, "key", types.ABCIPubKeyTypeEd25519,
		"Signer key type (ed25519 only)")

	resetCmd.AddCommand(resetBlocksCmd)
	resetCmd.AddCommand(resetPeersCmd)
	resetCmd.AddCommand(resetSignerCmd)
	resetCmd.AddCommand(resetAllCmd)

	return resetCmd
}

// ResetAll removes address book files plus all data, and resets the privValdiator data.
// Exported for extenal CLI usage
// XXX: this is unsafe and should only suitable for testnets.
func ResetAll(dbDir, privValKeyFile, privValStateFile string, keyType string, homeDir string) error {
	if err := os.RemoveAll(filepath.Join(homeDir, dbDir)); err == nil {
		logger.Info("Removed all blockchain history", "dir", dbDir)
	} else {
		logger.Error("error removing all blockchain history", "dir", dbDir, "err", err)
	}

	if err := tmos.EnsureDir(filepath.Join(homeDir, dbDir), 0700); err != nil {
		logger.Error("unable to recreate dbDir", "err", err)
	} else {
		logger.Info("Removed dbDir")
	}

	wasmDir := filepath.Join(homeDir, wasmDirName)
	if err := os.RemoveAll(wasmDir); err == nil {
		logger.Info("Removed wasm directory", "dir", wasmDir)
	} else {
		logger.Error("error removing wasm directory", "dir", wasmDir, "err", err)
	}

	// recreate the dbDir since the privVal state needs to live there
	return ResetFilePV(filepath.Join(homeDir, privValKeyFile), filepath.Join(homeDir, privValStateFile), keyType)
}

// removeIfExists removes a path if it exists, logging the result.
func removeIfExists(path, label string) {
	if tmos.FileExists(path) {
		if err := os.RemoveAll(path); err == nil {
			logger.Info("Removed "+label, "dir", path)
		} else {
			logger.Error("error removing "+label, "dir", path, "err", err)
		}
	}
}

// ResetState removes all blocks, tendermint state, indexed transactions and evidence.
// It handles both the legacy flat layout and the new subdirectory layout.
func ResetState(dbDir string) error {
	// Legacy paths (flat under data/)
	removeIfExists(filepath.Join(dbDir, "blockstore.db"), "blockstore.db")
	removeIfExists(filepath.Join(dbDir, "state.db"), "state.db")
	removeIfExists(filepath.Join(dbDir, "cs.wal"), "cs.wal")
	removeIfExists(filepath.Join(dbDir, "evidence.db"), "evidence.db")
	removeIfExists(filepath.Join(dbDir, "tx_index.db"), "tx_index.db")

	// New paths (subdirectory layout — all tendermint DBs under data/tendermint/)
	removeIfExists(filepath.Join(dbDir, "tendermint", "blockstore.db"), "tendermint/blockstore.db")
	removeIfExists(filepath.Join(dbDir, "tendermint", "tx_index.db"), "tendermint/tx_index.db")
	removeIfExists(filepath.Join(dbDir, "tendermint", "state.db"), "tendermint/state.db")
	removeIfExists(filepath.Join(dbDir, "tendermint", "cs.wal"), "tendermint/cs.wal")
	removeIfExists(filepath.Join(dbDir, "tendermint", "evidence.db"), "tendermint/evidence.db")
	removeIfExists(filepath.Join(dbDir, "tendermint", "peerstore.db"), "tendermint/peerstore.db")

	return tmos.EnsureDir(dbDir, 0700)
}

// ResetFilePV loads the file private validator and resets the watermark to 0. If used on an existing network,
// this can cause the node to double sign.
// XXX: this is unsafe and should only suitable for testnets.
func ResetFilePV(privValKeyFile, privValStateFile string, keyType string) error {
	if _, err := os.Stat(privValKeyFile); err == nil {
		pv, err := privval.LoadFilePVEmptyState(privValKeyFile, privValStateFile)
		if err != nil {
			return err
		}
		if err := pv.Reset(); err != nil {
			return err
		}
		logger.Info("Reset private validator file to genesis state", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	} else {
		pv, err := privval.GenFilePV(privValKeyFile, privValStateFile, keyType)
		if err != nil {
			return err
		}
		if err := pv.Save(); err != nil {
			return err
		}
		logger.Info("Generated private validator file", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	}
	return nil
}

// ResetPeerStore removes the peer store containing all information used by the tendermint networking layer.
// In the case of a reset, new peers will need to be set either via the config or through the discovery mechanism.
// It checks both legacy (data/peerstore.db) and new (data/tendermint/peerstore.db) locations.
func ResetPeerStore(dbDir string) error {
	legacy := filepath.Join(dbDir, "peerstore.db")
	if tmos.FileExists(legacy) {
		if err := os.RemoveAll(legacy); err != nil {
			return err
		}
	}
	newPath := filepath.Join(dbDir, "tendermint", "peerstore.db")
	if tmos.FileExists(newPath) {
		if err := os.RemoveAll(newPath); err != nil {
			return err
		}
	}
	return nil
}

func MakeUnsafeResetAllCommand(conf *config.Config) *cobra.Command {
	var keyType string

	resetAllCmd := &cobra.Command{
		Use:   "unsafe-reset-all",
		Short: "Removes all tendermint data, wasm directory, and signing state",
		Long: `Removes all tendermint data including signing state, and the wasm directory (contract blobs and compiled modules).
Only use in testing. This can cause the node to double sign`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get the --home flag value from the command
			home, err := cmd.Flags().GetString(cli.HomeFlag)
			if err != nil {
				return err
			}

			// If home is empty, use conf.RootDir as a fallback
			if home == "" {
				home = conf.RootDir
			}

			return ResetAll(conf.DBDir(), conf.PrivValidator.KeyFile(),
				conf.PrivValidator.StateFile(), keyType, home)
		},
	}

	resetAllCmd.Flags().StringVar(&keyType, "key", types.ABCIPubKeyTypeEd25519,
		"Signer key type (ed25519 only)")

	return resetAllCmd
}
