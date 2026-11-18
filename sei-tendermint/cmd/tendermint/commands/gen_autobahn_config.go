package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/app/upgradenames"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands/preinstalls"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// MakeGenAutobahnConfigCommand creates a cobra command that generates an autobahn JSON config file.
// Each node directory must contain validator_pubkey.txt, node_pubkey.txt,
// autobahn_address.txt, and evmrpc_url.txt, and evm_voter.txt when EVM
// governance is enabled.
func MakeGenAutobahnConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-autobahn-config [node-dirs...]",
		Short: "Generate autobahn JSON config from node pubkey files",
		Long: `Generate an autobahn JSON config file by reading validator_pubkey.txt,
node_pubkey.txt, autobahn_address.txt, and evmrpc_url.txt from each node
directory. With --evm-governance-voting-period, each directory must also contain
evm_voter.txt, the EVM address that votes for that validator.
Output is written to the file specified by --output.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			if output == "" {
				return fmt.Errorf("--output flag is required")
			}
			evmChainID, _ := cmd.Flags().GetUint64("evm-chain-id")
			if evmChainID == 0 {
				return fmt.Errorf("--evm-chain-id flag is required")
			}
			persistentStateDir, _ := cmd.Flags().GetString("persistent-state-dir")
			if persistentStateDir == "" {
				return fmt.Errorf("persistent-state-dir is required")
			}
			blockDBRetention, _ := cmd.Flags().GetString("blockdb-retention")
			blockDBGCPeriod, _ := cmd.Flags().GetString("blockdb-gc-period")
			governancePeriod, _ := cmd.Flags().GetDuration("evm-governance-voting-period")
			startingProposalID, _ := cmd.Flags().GetUint64("evm-governance-starting-proposal-id")
			governance, err := buildGenEVMGovernance(governancePeriod, startingProposalID)
			if err != nil {
				return err
			}

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

				validator := config.AutobahnValidator{
					ValidatorKey: valKey,
					NodeKey:      nodeKey,
					Address:      addr,
					EVMRPC:       evmRPC,
				}
				if governance.IsPresent() {
					voterRaw, err := os.ReadFile(filepath.Join(dir, "evm_voter.txt")) //nolint:gosec // G304: dir comes from command args; filepath.Join already calls Clean
					if err != nil {
						return fmt.Errorf("reading evm_voter.txt from %s: %w", dir, err)
					}
					voter := strings.TrimSpace(string(voterRaw))
					if !common.IsHexAddress(voter) {
						return fmt.Errorf("parsing evm voter from %s: %q is not an address", dir, voter)
					}
					validator.EVMVoter = utils.Some(common.HexToAddress(voter))
				}
				validators = append(validators, validator)
			}

			cfg := config.AutobahnFileConfig{
				Validators:     validators,
				MaxTxsPerBlock: 2_000,
				BlockInterval:  utils.Duration(400 * time.Millisecond),
				// node/setup.go rootifies a relative path against cfg.RootDir at load time.
				PersistentStateDir: persistentStateDir,
				DialInterval:       utils.Duration(10 * time.Second),
				EVMChainID:         evmChainID,
			}
			blockDB, err := buildGenBlockDBConfig(blockDBRetention, blockDBGCPeriod)
			if err != nil {
				return err
			}
			cfg.BlockDB = blockDB
			cfg.EVMGovernance = governance
			allocPath, _ := cmd.Flags().GetString("evm-alloc")
			preinstallLevel, _ := cmd.Flags().GetString("evm-preinstalls")
			alloc, err := buildGenEVMAlloc(allocPath, preinstallLevel, evmChainID)
			if err != nil {
				return err
			}
			cfg.EVMAlloc = alloc
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("generated config: %w", err)
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
	cmd.Flags().Uint64("evm-chain-id", 0, "EIP-155 chain ID of the EVM-only chain (required)")
	cmd.Flags().String("persistent-state-dir", "data/autobahn", "directory to persist autobahn consensus state and BlockDB across restarts; relative paths are resolved against the node's --home dir")
	// Default 30s: this helper is used by docker/local clusters, not production
	// node bring-up. Pass --blockdb-retention= (empty) to omit block_db and keep
	// littblock's production default (24h).
	cmd.Flags().String("blockdb-retention", "30s", "BlockDB retention TTL written into block_db (default 30s for local/docker); pass empty to omit and keep littblock's 24h default")
	cmd.Flags().String("blockdb-gc-period", "", "optional BlockDB GC period (e.g. 10s); omit to keep littblock default")
	cmd.Flags().Duration("evm-governance-voting-period", 0, "enable EVM governance with x/gov's default params, this voting and deposit period (at least 2s) and half of it for expedited voting, reading each node's evm_voter.txt; 0 disables it")
	cmd.Flags().Uint64("evm-governance-starting-proposal-id", 0, "id of the first EVM governance proposal; 0 means 1")
	cmd.Flags().String("evm-alloc", "", "JSON file of genesis accounts, address to {balance, nonce, code, storage}, written into evm_alloc; omit to start every account with 2^200 wei")
	cmd.Flags().String("evm-preinstalls", "", fmt.Sprintf("standard contracts written into evm_alloc, one of %v; empty means full when --evm-alloc funds an account and none otherwise", preinstalls.Levels()))
	return cmd
}

// buildGenEVMAlloc returns the accounts in the JSON file at path, when set, and
// the contracts of the preinstall level for chainID, or None when there are no
// accounts. An empty level means preinstalls.Full when the file funds an account
// and preinstalls.None otherwise. Any other level needs a funded account, and a
// file account at a preinstall's address is refused.
func buildGenEVMAlloc(path, level string, chainID uint64) (utils.Option[map[common.Address]config.EVMGenesisAccount], error) {
	none := utils.None[map[common.Address]config.EVMGenesisAccount]()
	alloc := map[common.Address]config.EVMGenesisAccount{}
	if path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // G304: path comes from a command flag
		if err != nil {
			return none, fmt.Errorf("reading --evm-alloc: %w", err)
		}
		if err := json.Unmarshal(data, &alloc); err != nil {
			return none, fmt.Errorf("parsing --evm-alloc %s: %w", path, err)
		}
	}
	funded := slices.ContainsFunc(slices.Collect(maps.Values(alloc)), func(a config.EVMGenesisAccount) bool { return a.Balance.IsPresent() })
	if len(alloc) > 0 && !funded {
		return none, fmt.Errorf("--evm-alloc %s funds no account", path)
	}
	if level == "" {
		level = preinstalls.None
		if funded {
			level = preinstalls.Full
		}
	}
	if level != preinstalls.None && !funded {
		return none, fmt.Errorf("--evm-preinstalls %s needs --evm-alloc to fund an account", level)
	}
	accounts, err := preinstalls.Accounts(level, new(big.Int).SetUint64(chainID))
	if err != nil {
		return none, fmt.Errorf("--evm-preinstalls: %w", err)
	}
	for _, account := range accounts {
		if _, ok := alloc[account.Address]; ok {
			return none, fmt.Errorf("--evm-alloc sets %s, preinstall %s", account.Address, account.Name)
		}
		alloc[account.Address] = config.EVMGenesisAccount{Nonce: hexutil.Uint64(account.Nonce), Code: account.Code, Storage: account.Storage}
	}
	if len(alloc) == 0 {
		return none, nil
	}
	return utils.Some(alloc), nil
}

// buildGenBlockDBConfig builds optional block_db overrides from gen-autobahn-config flags.
// Empty duration strings leave BlockDB as the zero value (omitted from JSON).
func buildGenBlockDBConfig(retention, gcPeriod string) (config.AutobahnBlockDBConfig, error) {
	var bdb config.AutobahnBlockDBConfig
	if retention != "" {
		d, err := time.ParseDuration(retention)
		if err != nil {
			return config.AutobahnBlockDBConfig{}, fmt.Errorf("--blockdb-retention: %w", err)
		}
		bdb.Retention = utils.Some(utils.Duration(d))
	}
	if gcPeriod != "" {
		d, err := time.ParseDuration(gcPeriod)
		if err != nil {
			return config.AutobahnBlockDBConfig{}, fmt.Errorf("--blockdb-gc-period: %w", err)
		}
		bdb.GCPeriod = utils.Some(utils.Duration(d))
	}
	if err := bdb.Validate(); err != nil {
		return config.AutobahnBlockDBConfig{}, fmt.Errorf("block_db: %w", err)
	}
	return bdb, nil
}

// buildGenEVMGovernance returns x/gov's default params with votingPeriod as the
// voting and deposit period and half of it as the expedited voting period, or
// None when votingPeriod is 0.
func buildGenEVMGovernance(votingPeriod time.Duration, startingProposalID uint64) (utils.Option[config.EVMGovernance], error) {
	if votingPeriod == 0 {
		if startingProposalID != 0 {
			return utils.None[config.EVMGovernance](), errors.New("--evm-governance-starting-proposal-id needs --evm-governance-voting-period")
		}
		return utils.None[config.EVMGovernance](), nil
	}
	if votingPeriod < 2*time.Second {
		return utils.None[config.EVMGovernance](), fmt.Errorf("--evm-governance-voting-period %s must be at least 2s", votingPeriod)
	}
	params := gov.DefaultParams()
	params.VotingPeriod = uint64(votingPeriod)
	params.MaxDepositPeriod = params.VotingPeriod
	params.ExpeditedVotingPeriod = params.VotingPeriod / 2
	return utils.Some(config.EVMGovernance{
		Params:             config.NewEVMGovernanceParams(params),
		StartingProposalID: startingProposalID,
		AppliedUpgrades:    upgradenames.Release(),
	}), nil
}
