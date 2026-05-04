package config

import (
	"errors"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// AutobahnValidator represents a validator entry in the autobahn config file.
type AutobahnValidator struct {
	ValidatorKey atypes.PublicKey  `json:"validator_key"`
	NodeKey      p2p.NodePublicKey `json:"node_key"`
	Address      tcp.HostPort      `json:"address"`
}

// AutobahnFileConfig is the JSON structure of the autobahn config file.
type AutobahnFileConfig struct {
	Validators         []AutobahnValidator  `json:"validators"`
	MaxTxsPerBlock     uint64               `json:"max_txs_per_block"`
	MaxTxsPerSecond    utils.Option[uint64] `json:"max_txs_per_second"`
	MempoolSize        uint64               `json:"mempool_size"`
	BlockInterval      utils.Duration       `json:"block_interval"`
	AllowEmptyBlocks   bool                 `json:"allow_empty_blocks"`
	ViewTimeout        utils.Duration       `json:"view_timeout"`
	PersistentStateDir utils.Option[string] `json:"persistent_state_dir"`
	DialInterval       utils.Duration       `json:"dial_interval"`
	// DataPruneAfter is the age at which data.State drops in-memory blocks,
	// QCs, and AppProposals; nil disables time-based pruning.
	DataPruneAfter utils.Option[utils.Duration] `json:"data_prune_after"`
}

// Validate performs basic validation of the autobahn file config.
func (fc *AutobahnFileConfig) Validate() error {
	if len(fc.Validators) == 0 {
		return errors.New("validators must not be empty")
	}
	if fc.MaxTxsPerBlock == 0 {
		return errors.New("max_txs_per_block must be > 0")
	}
	if fc.MempoolSize == 0 {
		return errors.New("mempool_size must be > 0")
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
