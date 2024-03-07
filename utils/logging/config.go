package logging

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	FeaturesEnabled string `mapstructure:"features_enabled"`
}

var DefaultConfig = Config{
	FeaturesEnabled: "",
}

const (
	flagFeaturesEnabled = "evm.features_enabled"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagFeaturesEnabled); v != nil {
		if cfg.FeaturesEnabled, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
