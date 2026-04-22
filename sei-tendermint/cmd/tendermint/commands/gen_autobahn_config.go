package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// MakeGenAutobahnConfigCommand creates a cobra command that generates an autobahn JSON config file.
// Each node directory must contain validator_pubkey.txt, node_pubkey.txt, and autobahn_address.txt.
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

			var validators []config.AutobahnValidator
			for _, dir := range args {
				valKeyRaw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "validator_pubkey.txt")))
				if err != nil {
					return fmt.Errorf("reading validator_pubkey.txt from %s: %w", dir, err)
				}
				var valKey atypes.PublicKey
				if err := valKey.UnmarshalText([]byte(strings.TrimSpace(string(valKeyRaw)))); err != nil {
					return fmt.Errorf("parsing validator key from %s: %w", dir, err)
				}

				nodeKeyRaw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "node_pubkey.txt")))
				if err != nil {
					return fmt.Errorf("reading node_pubkey.txt from %s: %w", dir, err)
				}
				var nodeKey p2p.NodePublicKey
				if err := nodeKey.UnmarshalText([]byte(strings.TrimSpace(string(nodeKeyRaw)))); err != nil {
					return fmt.Errorf("parsing node key from %s: %w", dir, err)
				}

				addrRaw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "autobahn_address.txt")))
				if err != nil {
					return fmt.Errorf("reading autobahn_address.txt from %s: %w", dir, err)
				}
				addr, err := tcp.ParseHostPort(strings.TrimSpace(string(addrRaw)))
				if err != nil {
					return fmt.Errorf("parsing address from %s: %w", dir, err)
				}

				validators = append(validators, config.AutobahnValidator{
					ValidatorKey: valKey,
					NodeKey:      nodeKey,
					Address:      addr,
				})
			}

			cfg := config.AutobahnFileConfig{
				Validators:       validators,
				MaxGasPerBlock:   50_000_000,
				MaxTxsPerBlock:   5_000,
				MempoolSize:      5_000,
				BlockInterval:    utils.Duration(400 * time.Millisecond),
				AllowEmptyBlocks: true,
				ViewTimeout:      utils.Duration(1500 * time.Millisecond),
				DialInterval:     utils.Duration(10 * time.Second),
			}

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
	return cmd
}
