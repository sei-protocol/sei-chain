package config

import (
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/spf13/cast"
)

// Config defines configuration for the Giga Executor
type Config struct {
	// Enabled controls whether to use the Giga executor
	Enabled bool `mapstructure:"enabled"`
	// OCCEnabled controls whether to use OCC (Optimistic Concurrency Control) with the Giga executor
	OCCEnabled bool `mapstructure:"occ_enabled"`
}

var DefaultConfig = Config{
	Enabled:    true,
	OCCEnabled: true,
}

const (
	FlagEnabled    = "giga_executor.enabled"
	FlagOCCEnabled = "giga_executor.occ_enabled"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(FlagEnabled); v != nil {
		if cfg.Enabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(FlagOCCEnabled); v != nil {
		if cfg.OCCEnabled, err = cast.ToBoolE(v); err != nil {
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
# enabled controls whether to use the Giga executor for improved EVM throughput.
# Default: true
enabled = {{ .GigaExecutor.Enabled }}

# occ_enabled controls whether to use OCC (Optimistic Concurrency Control) with the Giga executor.
# When true, transactions are executed in parallel with conflict detection and retry.
# Default: true
occ_enabled = {{ .GigaExecutor.OCCEnabled }}
`
