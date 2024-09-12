package keeper

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	DenomAllowListMaxSize int `mapstructure:"denom_allow_list_max_size"`
}

var DefaultConfig = Config{
	DenomAllowListMaxSize: 2000,
}

const (
	flagDenomAllowListMaxSize = "tokenfactory.denom_allow_list_max_size"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagDenomAllowListMaxSize); v != nil {
		if cfg.DenomAllowListMaxSize, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
