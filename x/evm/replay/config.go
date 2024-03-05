package replay

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	Enabled              bool   `mapstructure:"enabled"`
	EthRPC               string `mapstructure:"eth_rpc"`
	EthDataDir           string `mapstructure:"eth_data_dir"`
	EthDataEarliestBlock uint64 `mapstructure:"eth_data_earliest_block"`
	NumBlocksToReplay    uint64 `mapstructure:"num_blocks_to_replay"`
}

var DefaultConfig = Config{
	Enabled:              true,
	EthRPC:               "http://44.234.105.54:18545",
	EthDataDir:           "/root/.ethereum/chaindata",
	EthDataEarliestBlock: 19380498,
	NumBlocksToReplay:    10,
}

const (
	flagEnabled              = "eth_replay.enabled"
	flagEthRPC               = "eth_replay.eth_rpc"
	flagEthDataDir           = "eth_replay.eth_data_dir"
	flagEthDataEarliestBlock = "eth_replay.eth_data_earliest_block"
	flagNumBlocksToReplay    = "eth_replay.num_blocks_to_replay"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagEnabled); v != nil {
		if cfg.Enabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagEthRPC); v != nil {
		if cfg.EthRPC, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagEthDataDir); v != nil {
		if cfg.EthDataDir, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagEthDataEarliestBlock); v != nil {
		if cfg.EthDataEarliestBlock, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagNumBlocksToReplay); v != nil {
		if cfg.NumBlocksToReplay, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
