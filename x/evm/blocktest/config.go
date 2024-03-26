package blocktest

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

type Config struct {
	Enabled      bool   `mapstructure:"eth_blocktest_enabled"`
	TestDataPath string `mapstructure:"eth_blocktest_test_data_path"`
}

var DefaultConfig = Config{
	Enabled:      false,
	TestDataPath: "~/testdata/",
}

const (
	flagEnabled      = "eth_blocktest.eth_blocktest_enabled"
	flagTestDataPath = "eth_blocktest.eth_blocktest_test_data_path"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagEnabled); v != nil {
		if cfg.Enabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTestDataPath); v != nil {
		if cfg.TestDataPath, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
