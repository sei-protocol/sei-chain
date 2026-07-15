package config

import (
	"errors"
	"fmt"
	"net/url"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

type URL struct{ *url.URL }

func (u URL) MarshalText() ([]byte, error) { return []byte(u.String()), nil }
func (u *URL) UnmarshalText(text []byte) error {
	url, err := url.Parse(string(text))
	if err != nil {
		return err
	}
	u.URL = url
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
	Validators         []AutobahnValidator  `json:"validators"`
	MaxTxsPerBlock     uint64               `json:"max_txs_per_block"`
	MaxTxsPerSecond    utils.Option[uint64] `json:"max_txs_per_second"`
	AllowEmptyBlocks   bool                 `json:"allow_empty_blocks"`
	BlockInterval      utils.Duration       `json:"block_interval"`
	ViewTimeout        utils.Duration       `json:"view_timeout"`
	PersistentStateDir utils.Option[string] `json:"persistent_state_dir"`
	DialInterval       utils.Duration       `json:"dial_interval"`
	// MaxInboundFullnodePeers caps concurrent inbound block-sync from
	// non-committee peers, applied on both validators and fullnodes (relay
	// fullnodes serving downstream block-sync are subject to the same
	// cap). Absent ⇒ DefaultMaxInboundFullnodePeers. Some(0) ⇒ reject all.
	MaxInboundFullnodePeers utils.Option[uint64] `json:"max_inbound_fullnode_peers"`
	// BlockDB optionally overlays AutobahnBlockDBConfig onto littblock.DefaultConfig
	// when PersistentStateDir is set. Absent ⇒ littblock.DefaultConfig unchanged
	// (see AutobahnBlockDBConfig for field semantics). Ignored when
	// PersistentStateDir is absent (memblock).
	BlockDB utils.Option[AutobahnBlockDBConfig] `json:"block_db"`
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
	}
	if fc.MaxTxsPerBlock == 0 {
		return errors.New("max_txs_per_block must be > 0")
	}
	if fc.BlockInterval <= 0 {
		return errors.New("block_interval must be > 0")
	}
	if fc.ViewTimeout <= 0 {
		return errors.New("view_timeout must be > 0")
	}
	if fc.DialInterval <= 0 {
		return errors.New("dial_interval must be > 0")
	}
	if bdb, ok := fc.BlockDB.Get(); ok {
		if err := bdb.Validate(); err != nil {
			return fmt.Errorf("block_db: %w", err)
		}
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
