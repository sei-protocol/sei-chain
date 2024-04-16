package config

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	ChainID int64 `mapstructure:"evm_chain_id"`
}

var DefaultConfig = Config{
	ChainID: 713715,
}

const (
	flagChainID = "evm_module.evm_chain_id"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagChainID); v != nil {
		if cfg.ChainID, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
