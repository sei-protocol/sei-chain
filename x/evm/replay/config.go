package replay

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	Enabled    bool   `mapstructure:"eth_replay_enabled"`
	EthRPC     string `mapstructure:"eth_rpc"`
	EthDataDir string `mapstructure:"eth_data_dir"`
	ContractStateChecks bool `mapstructure:"contract_state_checks"`
}

var DefaultConfig = Config{
	Enabled:    false,
	EthRPC:     "http://44.234.105.54:18545",
	EthDataDir: "/root/.ethereum/chaindata",
	ContractStateChecks: false,
}

const (
	flagEnabled    = "eth_replay.eth_replay_enabled"
	flagEthRPC     = "eth_replay.eth_rpc"
	flagEthDataDir = "eth_replay.eth_data_dir"
	flagContractStateChecks = "eth_replay.contract_state_checks"
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
	if v := opts.Get(flagContractStateChecks); v != nil {
		if cfg.ContractStateChecks, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
