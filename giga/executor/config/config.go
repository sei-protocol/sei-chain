package config

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

// Config defines configuration for the Giga Executor
type Config struct {
	// Enabled controls whether to use the Giga executor (evmone-based) instead of geth's interpreter
	Enabled bool `mapstructure:"enabled"`
}

var DefaultConfig = Config{
	Enabled: false, // disabled by default, opt-in
}

const (
	flagEnabled = "giga_executor.enabled"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagEnabled); v != nil {
		if cfg.Enabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// ConfigTemplate defines the TOML configuration template for Giga Executor
const ConfigTemplate = `
###############################################################################
###                       Giga Executor Configuration                       ###
###############################################################################

[giga_executor]
# enabled controls whether to use the Giga executor (evmone-based) instead of geth's interpreter.
# This is an experimental feature for improved EVM throughput.
# Default: false
enabled = {{ .GigaExecutor.Enabled }}
`


