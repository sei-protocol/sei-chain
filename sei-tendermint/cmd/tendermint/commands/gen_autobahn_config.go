package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// MakeGenAutobahnConfigCommand creates a cobra command that generates an autobahn JSON config file.
// Each node directory must contain validator_pubkey.txt, node_pubkey.txt,
// autobahn_address.txt, and evmrpc_url.txt.
func MakeGenAutobahnConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-autobahn-config [node-dirs...]",
		Short: "Generate autobahn JSON config from node pubkey files",
		Long: `Generate an autobahn JSON config file by reading validator_pubkey.txt,
node_pubkey.txt, and autobahn_address.txt from each node directory.
Output is written to the file specified by --output.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			if output == "" {
				return fmt.Errorf("--output flag is required")
			}
			persistentStateDir, _ := cmd.Flags().GetString("persistent-state-dir")
			blockDBRetention, _ := cmd.Flags().GetString("blockdb-retention")
			blockDBGCPeriod, _ := cmd.Flags().GetString("blockdb-gc-period")
			blockDBFsyncSet := cmd.Flags().Changed("blockdb-fsync")
			blockDBFsync, _ := cmd.Flags().GetBool("blockdb-fsync")

			var validators []config.AutobahnValidator
			for _, dir := range args {
				valKeyRaw, err := os.ReadFile(filepath.Join(dir, "validator_pubkey.txt")) //nolint:gosec // G304: dir comes from command args; filepath.Join already calls Clean
				if err != nil {
					return fmt.Errorf("reading validator_pubkey.txt from %s: %w", dir, err)
				}
				var valKey atypes.PublicKey
				if err := valKey.UnmarshalText([]byte(strings.TrimSpace(string(valKeyRaw)))); err != nil {
					return fmt.Errorf("parsing validator key from %s: %w", dir, err)
				}

				nodeKeyRaw, err := os.ReadFile(filepath.Join(dir, "node_pubkey.txt")) //nolint:gosec // G304: dir comes from command args; filepath.Join already calls Clean
				if err != nil {
					return fmt.Errorf("reading node_pubkey.txt from %s: %w", dir, err)
				}
				var nodeKey p2p.NodePublicKey
				if err := nodeKey.UnmarshalText([]byte(strings.TrimSpace(string(nodeKeyRaw)))); err != nil {
					return fmt.Errorf("parsing node key from %s: %w", dir, err)
				}

				addrRaw, err := os.ReadFile(filepath.Join(dir, "autobahn_address.txt")) //nolint:gosec // G304: dir comes from command args; filepath.Join already calls Clean
				if err != nil {
					return fmt.Errorf("reading autobahn_address.txt from %s: %w", dir, err)
				}
				addr, err := tcp.ParseHostPort(strings.TrimSpace(string(addrRaw)))
				if err != nil {
					return fmt.Errorf("parsing address from %s: %w", dir, err)
				}

				evmRPCRaw, err := os.ReadFile(filepath.Join(dir, "evmrpc_url.txt")) //nolint:gosec // G304: dir comes from command args; filepath.Join already calls Clean
				if err != nil {
					return fmt.Errorf("reading evmrpc_url.txt from %s: %w", dir, err)
				}
				var evmRPC config.URL
				if err := evmRPC.UnmarshalText([]byte(strings.TrimSpace(string(evmRPCRaw)))); err != nil {
					return fmt.Errorf("parsing evmrpc URL from %s: %w", dir, err)
				}

				validators = append(validators, config.AutobahnValidator{
					ValidatorKey: valKey,
					NodeKey:      nodeKey,
					Address:      addr,
					EVMRPC:       evmRPC,
				})
			}

			cfg := config.AutobahnFileConfig{
				Validators:       validators,
				MaxTxsPerBlock:   5_000,
				AllowEmptyBlocks: true,
				BlockInterval:    utils.Duration(400 * time.Millisecond),
				ViewTimeout:      utils.Duration(1500 * time.Millisecond),
				DialInterval:     utils.Duration(10 * time.Second),
			}
			// The flag defaults to "data/autobahn" so persistence is on without
			// operator action. node/setup.go rootifies the relative path against
			// cfg.RootDir at load time. Passing --persistent-state-dir= (empty)
			// disables persistence and runs both consensus and data layers
			// in-memory only.
			if persistentStateDir != "" {
				cfg.PersistentStateDir = utils.Some(persistentStateDir)
			}
			blockDB, err := buildGenBlockDBConfig(blockDBRetention, blockDBGCPeriod, blockDBFsyncSet, blockDBFsync)
			if err != nil {
				return err
			}
			cfg.BlockDB = blockDB

			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling config: %w", err)
			}
			if err := os.WriteFile(output, data, 0600); err != nil {
				return fmt.Errorf("writing config to %s: %w", output, err)
			}
			fmt.Printf("Autobahn config written to %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "output file path for the autobahn config")
	cmd.Flags().String("persistent-state-dir", "data/autobahn", "directory to persist autobahn consensus and data WALs across restarts; relative paths are resolved against the node's --home dir; pass --persistent-state-dir= (empty) to disable persistence and run in-memory only")
	// Default 30s: this helper is used by docker/local clusters, not production
	// node bring-up. Pass --blockdb-retention= (empty) to omit block_db and keep
	// littblock's production default (24h).
	cmd.Flags().String("blockdb-retention", "30s", "BlockDB retention TTL written into block_db (default 30s for local/docker); pass empty to omit and keep littblock's 24h default")
	cmd.Flags().String("blockdb-gc-period", "", "optional BlockDB GC period (e.g. 10s); omit to keep littblock default")
	cmd.Flags().Bool("blockdb-fsync", true, "optional BlockDB fsync override; only written when the flag is explicitly set")
	return cmd
}

// buildGenBlockDBConfig builds optional block_db overrides from gen-autobahn-config flags.
// Empty duration strings and an unset fsync flag leave BlockDB as None.
func buildGenBlockDBConfig(retention, gcPeriod string, fsyncSet, fsync bool) (utils.Option[config.AutobahnBlockDBConfig], error) {
	var bdb config.AutobahnBlockDBConfig
	set := false
	if retention != "" {
		d, err := time.ParseDuration(retention)
		if err != nil {
			return utils.None[config.AutobahnBlockDBConfig](), fmt.Errorf("--blockdb-retention: %w", err)
		}
		bdb.Retention = utils.Some(utils.Duration(d))
		set = true
	}
	if gcPeriod != "" {
		d, err := time.ParseDuration(gcPeriod)
		if err != nil {
			return utils.None[config.AutobahnBlockDBConfig](), fmt.Errorf("--blockdb-gc-period: %w", err)
		}
		bdb.GCPeriod = utils.Some(utils.Duration(d))
		set = true
	}
	if fsyncSet {
		bdb.Fsync = utils.Some(fsync)
		set = true
	}
	if !set {
		return utils.None[config.AutobahnBlockDBConfig](), nil
	}
	if err := bdb.Validate(); err != nil {
		return utils.None[config.AutobahnBlockDBConfig](), fmt.Errorf("block_db: %w", err)
	}
	return utils.Some(bdb), nil
}
