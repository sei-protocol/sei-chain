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

// AutobahnFileConfig is the JSON structure of the autobahn config file.
type AutobahnFileConfig struct {
	Validators         []AutobahnValidator  `json:"validators"`
	MaxTxsPerBlock     uint64               `json:"max_txs_per_block"`
	MaxTxsPerSecond    utils.Option[uint64] `json:"max_txs_per_second"`
	AllowEmptyBlocks   bool                 `json:"allow_empty_blocks"`
	BlockInterval      utils.Duration       `json:"block_interval"`
	ViewTimeout        utils.Duration       `json:"view_timeout"`
	PersistentStateDir utils.Option[string] `json:"persistent_state_dir,omitzero"`
	DialInterval       utils.Duration       `json:"dial_interval"`
	// MaxInboundFullnodePeers caps concurrent inbound block-sync from
	// non-committee peers, applied on both validators and fullnodes (relay
	// fullnodes serving downstream block-sync are subject to the same
	// cap). Absent ⇒ DefaultMaxInboundFullnodePeers. Some(0) ⇒ reject all.
	MaxInboundFullnodePeers utils.Option[uint64] `json:"max_inbound_fullnode_peers,omitzero"`
	EnableEvmProxy          utils.Option[bool]   `json:"enable_evm_proxy,omitzero"`
}

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
	return nil
}
