package config

import (
	"errors"
	"net/url"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
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
	EVMRPC utils.Option[URL] `json:"evmrpc"`
}

func (av *AutobahnValidator) GetEVMRPC() utils.Option[*url.URL] {
	if u, ok := av.EVMRPC.Get(); ok {
		return utils.Some(u.URL)
	}
	return utils.None[*url.URL]()
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
}

// Validate performs basic validation of the autobahn file config.
func (fc *AutobahnFileConfig) Validate() error {
	if len(fc.Validators) == 0 {
		return errors.New("validators must not be empty")
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
