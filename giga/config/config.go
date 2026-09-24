// Package config defines the [giga] section of app.toml: the storage and execution settings a
// Giga (Autobahn) node runs with.
package config

import (
	"fmt"
	"time"

	"github.com/spf13/cast"
)

// AppOptions is the flat key-value view a section reader consumes. servertypes.AppOptions
// satisfies it structurally, so this package needs no import of sei-cosmos.
type AppOptions interface {
	Get(string) interface{}
}

// Storage modes an operator may pin. The empty string follows the node's own mode.
const (
	StorageModeAuto      = ""
	StorageModeValidator = "validator"
	StorageModeFull      = "full"
)

// Config is the [giga] section.
type Config struct {
	Storage   StorageConfig   `mapstructure:"storage"`
	Execution ExecutionConfig `mapstructure:"execution"`
}

// StorageConfig is the [giga.storage] sub-table: retention and layout of the Giga stores.
type StorageConfig struct {
	// Mode selects the store layout: "validator" skips the state store, "full" opens it for
	// serving queries. Empty follows the node's mode from config.toml.
	Mode string `mapstructure:"mode"`
	// RollbackWindow is how many blocks behind head the node must remain able to roll back to.
	RollbackWindow uint64 `mapstructure:"rollback_window"`
	// LookbackWindow is how many queryable blocks are kept below the rollback window; -1 keeps all.
	LookbackWindow int64 `mapstructure:"lookback_window"`
	// PruneInterval is how often the storage garbage collector runs.
	PruneInterval time.Duration `mapstructure:"prune_interval"`
	// CheckpointTimeInterval is the wall-clock gap between state-commit checkpoints.
	CheckpointTimeInterval time.Duration `mapstructure:"checkpoint_time_interval"`
	// CheckpointBlockInterval places checkpoints on multiples of itself; 0 disables the rule.
	CheckpointBlockInterval int64 `mapstructure:"checkpoint_block_interval"`
}

// ExecutionConfig is the [giga.execution] sub-table: how the EVM-only executor runs blocks.
type ExecutionConfig struct {
	// MinGasPrice is the lowest effective gas price, in wei, a transaction is admitted at.
	MinGasPrice uint64 `mapstructure:"min_gas_price"`
	// OCCWorkers is the number of parallel execution workers; 0 uses GOMAXPROCS.
	OCCWorkers int `mapstructure:"occ_workers"`
	// ParseWorkers is the number of parallel transaction decoding workers; 0 uses GOMAXPROCS.
	ParseWorkers int `mapstructure:"parse_workers"`
	// BlockResultPoolSize is the number of block results kept pooled between executions.
	BlockResultPoolSize int `mapstructure:"block_result_pool_size"`
}

// DefaultConfig is what a Giga node runs when the section is absent.
var DefaultConfig = Config{
	Storage: StorageConfig{
		Mode:                    StorageModeAuto,
		RollbackWindow:          1_000,
		LookbackWindow:          0,
		PruneInterval:           5 * time.Minute,
		CheckpointTimeInterval:  10 * time.Minute,
		CheckpointBlockInterval: 0,
	},
	Execution: ExecutionConfig{
		MinGasPrice:         1_000_000_000,
		OCCWorkers:          0,
		ParseWorkers:        0,
		BlockResultPoolSize: 1,
	},
}

// The keys this package's reader resolves.
const (
	FlagStorageMode                    = "giga.storage.mode"
	FlagStorageRollbackWindow          = "giga.storage.rollback_window"
	FlagStorageLookbackWindow          = "giga.storage.lookback_window"
	FlagStoragePruneInterval           = "giga.storage.prune_interval"
	FlagStorageCheckpointTimeInterval  = "giga.storage.checkpoint_time_interval"
	FlagStorageCheckpointBlockInterval = "giga.storage.checkpoint_block_interval"
	FlagExecutionMinGasPrice           = "giga.execution.min_gas_price"
	FlagExecutionOCCWorkers            = "giga.execution.occ_workers"
	FlagExecutionParseWorkers          = "giga.execution.parse_workers"
	FlagExecutionBlockResultPoolSize   = "giga.execution.block_result_pool_size"
)

// ReadConfig reads the [giga] section from app options. An absent key keeps its default.
func ReadConfig(opts AppOptions) (Config, error) {
	cfg := DefaultConfig
	var err error
	if v := opts.Get(FlagStorageMode); v != nil {
		if cfg.Storage.Mode, err = cast.ToStringE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageMode, err)
		}
	}
	if v := opts.Get(FlagStorageRollbackWindow); v != nil {
		if cfg.Storage.RollbackWindow, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageRollbackWindow, err)
		}
	}
	if v := opts.Get(FlagStorageLookbackWindow); v != nil {
		if cfg.Storage.LookbackWindow, err = cast.ToInt64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageLookbackWindow, err)
		}
	}
	if v := opts.Get(FlagStoragePruneInterval); v != nil {
		if cfg.Storage.PruneInterval, err = cast.ToDurationE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStoragePruneInterval, err)
		}
	}
	if v := opts.Get(FlagStorageCheckpointTimeInterval); v != nil {
		if cfg.Storage.CheckpointTimeInterval, err = cast.ToDurationE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageCheckpointTimeInterval, err)
		}
	}
	if v := opts.Get(FlagStorageCheckpointBlockInterval); v != nil {
		if cfg.Storage.CheckpointBlockInterval, err = cast.ToInt64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageCheckpointBlockInterval, err)
		}
	}
	if v := opts.Get(FlagExecutionMinGasPrice); v != nil {
		if cfg.Execution.MinGasPrice, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionMinGasPrice, err)
		}
	}
	if v := opts.Get(FlagExecutionOCCWorkers); v != nil {
		if cfg.Execution.OCCWorkers, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionOCCWorkers, err)
		}
	}
	if v := opts.Get(FlagExecutionParseWorkers); v != nil {
		if cfg.Execution.ParseWorkers, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionParseWorkers, err)
		}
	}
	if v := opts.Get(FlagExecutionBlockResultPoolSize); v != nil {
		if cfg.Execution.BlockResultPoolSize, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionBlockResultPoolSize, err)
		}
	}
	return cfg, cfg.Validate()
}

// Validate reports the first setting a Giga node cannot start with.
func (c Config) Validate() error {
	switch c.Storage.Mode {
	case StorageModeAuto, StorageModeValidator, StorageModeFull:
	default:
		return fmt.Errorf("%s: %q is not one of %q, %q or empty", FlagStorageMode, c.Storage.Mode,
			StorageModeValidator, StorageModeFull)
	}
	if c.Storage.LookbackWindow < -1 {
		return fmt.Errorf("%s: must be >= 0, or -1 to keep all history, got %d", FlagStorageLookbackWindow,
			c.Storage.LookbackWindow)
	}
	if c.Storage.PruneInterval <= 0 {
		return fmt.Errorf("%s: must be positive, got %s", FlagStoragePruneInterval, c.Storage.PruneInterval)
	}
	if c.Storage.CheckpointTimeInterval <= 0 {
		return fmt.Errorf("%s: must be positive, got %s", FlagStorageCheckpointTimeInterval,
			c.Storage.CheckpointTimeInterval)
	}
	if c.Storage.CheckpointBlockInterval < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagStorageCheckpointBlockInterval,
			c.Storage.CheckpointBlockInterval)
	}
	if c.Execution.OCCWorkers < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagExecutionOCCWorkers, c.Execution.OCCWorkers)
	}
	if c.Execution.ParseWorkers < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagExecutionParseWorkers, c.Execution.ParseWorkers)
	}
	if c.Execution.BlockResultPoolSize < 1 {
		return fmt.Errorf("%s: must be >= 1, got %d", FlagExecutionBlockResultPoolSize,
			c.Execution.BlockResultPoolSize)
	}
	return nil
}

// ConfigTemplate is the TOML template for the [giga] section of app.toml.
const ConfigTemplate = `
###############################################################################
###                          Giga Node Configuration                        ###
###############################################################################

# Storage and execution settings of a Giga (Autobahn) node. Committee and consensus
# settings stay in the file named by autobahn-config-file in config.toml.

[giga.storage]
# mode selects the store layout. "validator" skips the state store; "full" opens it
# for serving queries. Empty follows the node's mode in config.toml.
mode = "{{ .Giga.Storage.Mode }}"

# rollback_window is how many blocks behind head the node must remain able to roll back to.
rollback_window = {{ .Giga.Storage.RollbackWindow }}

# lookback_window is how many queryable blocks are kept below the rollback window.
# -1 keeps all history.
lookback_window = {{ .Giga.Storage.LookbackWindow }}

# prune_interval is how often the storage garbage collector runs.
prune_interval = "{{ .Giga.Storage.PruneInterval }}"

# checkpoint_time_interval is the wall-clock gap between state-commit checkpoints.
checkpoint_time_interval = "{{ .Giga.Storage.CheckpointTimeInterval }}"

# checkpoint_block_interval places checkpoints on multiples of itself; 0 disables the rule.
checkpoint_block_interval = {{ .Giga.Storage.CheckpointBlockInterval }}

[giga.execution]
# min_gas_price is the lowest effective gas price, in wei, a transaction is admitted at.
min_gas_price = {{ .Giga.Execution.MinGasPrice }}

# occ_workers is the number of parallel execution workers; 0 uses GOMAXPROCS.
occ_workers = {{ .Giga.Execution.OCCWorkers }}

# parse_workers is the number of parallel transaction decoding workers; 0 uses GOMAXPROCS.
parse_workers = {{ .Giga.Execution.ParseWorkers }}

# block_result_pool_size is the number of block results kept pooled between executions.
block_result_pool_size = {{ .Giga.Execution.BlockResultPoolSize }}
`
