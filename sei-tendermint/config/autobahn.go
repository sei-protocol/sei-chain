package config

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

type URL struct{ *url.URL }

func (u URL) MarshalText() ([]byte, error) { return []byte(u.String()), nil }
func (u *URL) UnmarshalText(text []byte) error {
	parsed, err := url.Parse(string(text))
	if err != nil {
		return err
	}
	if err := utils.CheckRPCURL(*parsed); err != nil {
		return err
	}
	u.URL = parsed
	return nil
}

// AutobahnValidator represents a validator entry in the autobahn config file.
type AutobahnValidator struct {
	ValidatorKey atypes.PublicKey  `json:"validator_key"`
	NodeKey      p2p.NodePublicKey `json:"node_key"`
	Address      tcp.HostPort      `json:"address"`
	// Each validator is assigned a shard of EVM address space.
	// Upon receiving an EVM transaction, a node needs to proxy it
	// to validator owning the shard.
	EVMRPC URL `json:"evmrpc"`
	// EVMVoter is the EVM address that votes for this validator in EVM
	// governance, read at every start. Required for every validator when
	// evm_governance is set, and forbidden otherwise.
	EVMVoter utils.Option[common.Address] `json:"evm_voter,omitzero"`
}

// EVMGovernance enables the EVM-only gov precompile. The chain's first block
// writes Params and StartingProposalID into state, so later edits to them have
// no effect.
type EVMGovernance struct {
	Params EVMGovernanceParams `json:"params"`
	// StartingProposalID is the id of the first proposal, so ids continue
	// past another chain's. Zero means 1.
	StartingProposalID uint64 `json:"starting_proposal_id,omitzero"`
}

// EVMGovernanceParams are gov's genesis params. Periods are duration strings
// such as "48h", deposits are wei, and ratios are 1e18-scaled decimals.
type EVMGovernanceParams struct {
	VotingPeriod          utils.Duration `json:"voting_period"`
	ExpeditedVotingPeriod utils.Duration `json:"expedited_voting_period"`
	MinDeposit            *big.Int       `json:"min_deposit"`
	MaxDepositPeriod      utils.Duration `json:"max_deposit_period"`
	MinExpeditedDeposit   *big.Int       `json:"min_expedited_deposit"`
	Quorum                uint64         `json:"quorum"`
	Threshold             uint64         `json:"threshold"`
	VetoThreshold         uint64         `json:"veto_threshold"`
	ExpeditedQuorum       uint64         `json:"expedited_quorum"`
	ExpeditedThreshold    uint64         `json:"expedited_threshold"`
}

// NewEVMGovernanceParams returns p as config params.
func NewEVMGovernanceParams(p gov.Params) EVMGovernanceParams {
	return EVMGovernanceParams{
		VotingPeriod:          period(p.VotingPeriod),
		ExpeditedVotingPeriod: period(p.ExpeditedVotingPeriod),
		MinDeposit:            p.MinDeposit,
		MaxDepositPeriod:      period(p.MaxDepositPeriod),
		MinExpeditedDeposit:   p.MinExpeditedDeposit,
		Quorum:                p.Quorum,
		Threshold:             p.Threshold,
		VetoThreshold:         p.VetoThreshold,
		ExpeditedQuorum:       p.ExpeditedQuorum,
		ExpeditedThreshold:    p.ExpeditedThreshold,
	}
}

// period returns ns nanoseconds as a Duration, capped at the longest one.
func period(ns uint64) utils.Duration {
	return utils.Duration(min(ns, math.MaxInt64)) //nolint:gosec // capped at MaxInt64
}

// Gov returns p as gov's params, failing on a negative period or params gov
// rejects.
func (p EVMGovernanceParams) Gov() (gov.Params, error) {
	params := gov.Params{
		MinDeposit:          p.MinDeposit,
		MinExpeditedDeposit: p.MinExpeditedDeposit,
		Quorum:              p.Quorum,
		Threshold:           p.Threshold,
		VetoThreshold:       p.VetoThreshold,
		ExpeditedQuorum:     p.ExpeditedQuorum,
		ExpeditedThreshold:  p.ExpeditedThreshold,
	}
	for _, d := range []struct {
		name string
		v    utils.Duration
		ns   *uint64
	}{
		{"voting_period", p.VotingPeriod, &params.VotingPeriod},
		{"expedited_voting_period", p.ExpeditedVotingPeriod, &params.ExpeditedVotingPeriod},
		{"max_deposit_period", p.MaxDepositPeriod, &params.MaxDepositPeriod},
	} {
		if d.v < 0 {
			return gov.Params{}, fmt.Errorf("%s %s is negative", d.name, d.v.Duration())
		}
		*d.ns = uint64(d.v) //nolint:gosec // checked non-negative
	}
	if err := params.Validate(); err != nil {
		return gov.Params{}, err
	}
	return params, nil
}

// AutobahnBlockDBConfig holds optional overrides for the LittDB-backed BlockDB
// opened under persistent_state_dir/blockdb. Paths are never taken from here —
// Autobahn always roots BlockDB at <persistent_state_dir>/blockdb.
//
// Each field is independently optional. Absent fields keep whatever
// littblock.DefaultConfig currently uses (do not duplicate those values here —
// they live in the littblock / LittDB packages and may change).
//
// Disk impact (most → least useful for bounding usage): Retention, then
// GCPeriod. Segment sizing is intentionally not exposed (engine-internal).
type AutobahnBlockDBConfig struct {
	// Retention is the failsafe minimum age before pruned records may be
	// reclaimed. Primary knob for worst-case disk after PruneBefore advances.
	// Absent ⇒ littblock.DefaultConfig Retention.
	Retention utils.Option[utils.Duration] `json:"retention"`
	// GCPeriod is how often GC runs once data is eligible (reclaim latency).
	// Absent ⇒ littblock.DefaultConfig / LittDB GCPeriod.
	GCPeriod utils.Option[utils.Duration] `json:"gc_period"`
}

// AutobahnFileConfig is the JSON structure of the autobahn config file.
type AutobahnFileConfig struct {
	Validators       []AutobahnValidator  `json:"validators"`
	MaxTxsPerBlock   uint64               `json:"max_txs_per_block"`
	MaxTxsPerSecond  utils.Option[uint64] `json:"max_txs_per_second"`
	AllowEmptyBlocks bool                 `json:"allow_empty_blocks"`
	BlockInterval    utils.Duration       `json:"block_interval"`
	// PersistentStateDir is the on-disk root for Autobahn's durable state
	// (Giga storage, BlockDB, hashvault, epoch snapshots, and the validator's
	// consensus persister, each in a subdirectory). A relative path is
	// resolved against the node's home dir. Required: every Autobahn node
	// runs on on-disk storage.
	PersistentStateDir string         `json:"persistent_state_dir"`
	DialInterval       utils.Duration `json:"dial_interval"`
	// MaxInboundFullnodePeers caps concurrent inbound block-sync from
	// non-committee peers, applied on both validators and fullnodes (relay
	// fullnodes serving downstream block-sync are subject to the same
	// cap). Absent ⇒ DefaultMaxInboundFullnodePeers. Some(0) ⇒ reject all.
	MaxInboundFullnodePeers utils.Option[uint64] `json:"max_inbound_fullnode_peers,omitzero"`
	// MaxConcurrentCheckTx caps the number of CheckTx calls the local mempool
	// runs concurrently for incoming broadcast_tx requests, so that ingest
	// cannot starve the consensus and data loops of CPU.
	// Absent ⇒ half of GOMAXPROCS (at least 1).
	MaxConcurrentCheckTx utils.Option[uint64] `json:"max_concurrent_check_tx,omitzero"`
	// Whether validators proxy mempool EVM RPC requests to the validator
	// handling a given shard of addresses.
	// No-op on fullnodes: they do not have a local mempool, so EVM RPC
	// requests are always proxied to the shard owner.
	// Useful for loadtesting (to compare enabled/disabled performance).
	// Defaults to true.
	EnableEvmProxy utils.Option[bool] `json:"enable_evm_proxy,omitzero"`
	// BlockDB optionally overlays AutobahnBlockDBConfig onto littblock.DefaultConfig.
	// Zero value ⇒ littblock.DefaultConfig unchanged (see AutobahnBlockDBConfig
	// for field semantics). Omitted from JSON when empty.
	BlockDB AutobahnBlockDBConfig `json:"block_db,omitzero"`
	// EVMGovernance enables the EVM-only gov precompile, voted on by the
	// validators' evm_voter addresses. Voters are read from this file at every
	// start and kept nowhere else, so this section and every evm_voter must be
	// identical on every node; a node that differs forks at its next tally.
	EVMGovernance utils.Option[EVMGovernance] `json:"evm_governance,omitzero"`
}

// AutobahnEVMOnlyChainID is the chain ID of the Autobahn EVM-only executor.
const AutobahnEVMOnlyChainID uint64 = 713715

func (c *AutobahnFileConfig) GetEnableEvmProxy() bool {
	return c.EnableEvmProxy.Or(true)
}

// DefaultMaxInboundFullnodePeers is the built-in cap used when
// AutobahnFileConfig.MaxInboundFullnodePeers is absent.
//
// TODO(autobahn-trusted-fullnode-peers): add an optional trusted-peer
// list whose keys bypass the cap.
const DefaultMaxInboundFullnodePeers = 10

// Validate performs basic validation of the autobahn file config.
func (fc *AutobahnFileConfig) Validate() error {
	if len(fc.Validators) == 0 {
		return errors.New("validators must not be empty")
	}
	for _, v := range fc.Validators {
		if v.EVMRPC.URL == nil {
			return fmt.Errorf("validator %s is missing evmrpc URL", v.ValidatorKey)
		}
		if err := utils.CheckRPCURL(*v.EVMRPC.URL); err != nil {
			return fmt.Errorf("validator %s evmrpc: %w", v.ValidatorKey, err)
		}
	}
	if fc.MaxTxsPerBlock == 0 {
		return errors.New("max_txs_per_block must be > 0")
	}
	if fc.BlockInterval <= 0 {
		return errors.New("block_interval must be > 0")
	}
	if fc.DialInterval <= 0 {
		return errors.New("dial_interval must be > 0")
	}
	if v, ok := fc.MaxConcurrentCheckTx.Get(); ok && (v == 0 || v > math.MaxInt32) {
		return fmt.Errorf("max_concurrent_check_tx must be in 1..%d when set", math.MaxInt32)
	}
	if fc.PersistentStateDir == "" {
		return errors.New("persistent_state_dir must not be empty")
	}
	if err := fc.BlockDB.Validate(); err != nil {
		return fmt.Errorf("block_db: %w", err)
	}
	if err := fc.validateEVMGovernance(); err != nil {
		return fmt.Errorf("evm_governance: %w", err)
	}
	return nil
}

// validateEVMGovernance checks that evm_governance's params are valid and that
// every validator has a distinct nonzero evm_voter exactly when it is set.
func (fc *AutobahnFileConfig) validateEVMGovernance() error {
	governance, enabled := fc.EVMGovernance.Get()
	if !enabled {
		for _, v := range fc.Validators {
			if v.EVMVoter.IsPresent() {
				return fmt.Errorf("validator %s sets evm_voter without evm_governance", v.ValidatorKey)
			}
		}
		return nil
	}
	if _, err := governance.Params.Gov(); err != nil {
		return err
	}
	voters := make(map[common.Address]bool, len(fc.Validators))
	for _, v := range fc.Validators {
		voter, ok := v.EVMVoter.Get()
		if !ok {
			return fmt.Errorf("validator %s is missing evm_voter", v.ValidatorKey)
		}
		if voter == (common.Address{}) {
			return fmt.Errorf("validator %s evm_voter is the zero address", v.ValidatorKey)
		}
		if voters[voter] {
			return fmt.Errorf("validator %s evm_voter %s is shared with another validator", v.ValidatorKey, voter)
		}
		voters[voter] = true
	}
	return nil
}

// Validate checks optional BlockDB overrides. Absent fields are fine.
func (c AutobahnBlockDBConfig) Validate() error {
	if r, ok := c.Retention.Get(); ok && r <= 0 {
		return errors.New("retention must be > 0 when set")
	}
	if p, ok := c.GCPeriod.Get(); ok && p <= 0 {
		return errors.New("gc_period must be > 0 when set")
	}
	return nil
}

// LittBlockConfig returns littblock.DefaultConfig(dir) with this config's
// optional overrides applied. Fsync is always forced on.
func (c AutobahnBlockDBConfig) LittBlockConfig(dir string) (littblock.BlockDBConfig, error) {
	if err := c.Validate(); err != nil {
		return littblock.BlockDBConfig{}, err
	}
	cfg, err := littblock.DefaultConfig(dir)
	if err != nil {
		return littblock.BlockDBConfig{}, fmt.Errorf("littblock.DefaultConfig: %w", err)
	}
	if r, ok := c.Retention.Get(); ok {
		cfg.RetentionTime = r.Duration()
	}
	if p, ok := c.GCPeriod.Get(); ok {
		cfg.Litt.GCPeriod = p.Duration()
	}
	// NOT SAFE to set false: crash can lose acknowledged BlockDB writes.
	cfg.Litt.Fsync = true
	return *cfg, nil
}
