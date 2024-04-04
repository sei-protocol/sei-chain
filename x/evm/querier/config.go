package querier

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	GasLimit uint64 `mapstructure:"evm_query_gas_limit"`
}

var DefaultConfig = Config{
	GasLimit: 300000,
}

const (
	flagGasLimit = "evm_query.evm_query_gas_limit"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagGasLimit); v != nil {
		if cfg.GasLimit, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
