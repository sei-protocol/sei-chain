package config

import (
	"fmt"
	"strings"

	"github.com/spf13/cast"
)

// AppOptions is a minimal interface for reading config (e.g. from Viper).
// Implemented by sei-cosmos server/types.AppOptions; defined here to avoid import cycles.
type AppOptions interface {
	Get(string) interface{}
}

const (
	flagRSBackend = "receipt-store.rs-backend"
)

// ReceiptStoreConfig defines configuration for the receipt store database.
type ReceiptStoreConfig struct {
	// DBDirectory defines the directory to store the receipt store db files
	// If not explicitly set, default to application home directory
	// default to empty
	DBDirectory string `mapstructure:"db-directory"`

	// Backend defines the backend database used for receipt-store.
	// Supported backends: pebbledb (aka pebble), parquet
	// defaults to pebbledb
	Backend string `mapstructure:"rs-backend"`

	// AsyncWriteBuffer defines the async queue length for commits to be applied to receipt store
	// Set <= 0 for synchronous writes.
	// defaults to 100
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// KeepRecent defines the number of versions to keep in receipt store
	// Setting it to 0 means keep everything.
	// Default to keep the last 100,000 blocks
	KeepRecent int `mapstructure:"keep-recent"`

	// PruneIntervalSeconds defines the interval in seconds to trigger pruning
	// default to every 600 seconds
	PruneIntervalSeconds int `mapstructure:"prune-interval-seconds"`

	// UseDefaultComparer uses Pebble's default lexicographic byte comparer instead of
	// the custom MVCCComparer. This is NOT backwards compatible with existing databases
	// that were created with MVCCComparer - only use this for NEW databases.
	// defaults to false (use MVCCComparer for backwards compatibility)
	UseDefaultComparer bool `mapstructure:"use-default-comparer"`
}

// DefaultReceiptStoreConfig returns the default ReceiptStoreConfig
func DefaultReceiptStoreConfig() ReceiptStoreConfig {
	return ReceiptStoreConfig{
		Backend:              "pebbledb",
		AsyncWriteBuffer:     DefaultSSAsyncBuffer,
		KeepRecent:           DefaultSSKeepRecent,
		PruneIntervalSeconds: DefaultSSPruneInterval,
		UseDefaultComparer:   false,
	}
}

// ReadReceiptConfig reads receipt store config from app options (e.g. TOML / Viper).
func ReadReceiptConfig(opts AppOptions) (ReceiptStoreConfig, error) {
	cfg := DefaultReceiptStoreConfig()
	if v := opts.Get(flagRSBackend); v != nil {
		backend, err := cast.ToStringE(v)
		if err != nil {
			return cfg, err
		}
		backend = strings.ToLower(strings.TrimSpace(backend))
		switch backend {
		case "pebbledb", "pebble", "parquet":
			cfg.Backend = backend
		default:
			return cfg, fmt.Errorf("unsupported receipt-store backend %q; supported: pebbledb, parquet", backend)
		}
	}
	return cfg, nil
}
