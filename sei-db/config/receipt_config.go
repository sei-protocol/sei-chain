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
	flagRSDBDirectory          = "receipt-store.db-directory"
	flagRSBackend              = "receipt-store.rs-backend"
	flagRSMisnamedBackend      = "receipt-store.backend"
	flagRSAsyncWriteBuffer     = "receipt-store.async-write-buffer"
	flagRSPruneIntervalSeconds = "receipt-store.prune-interval-seconds"
	flagRSReadWriteMetrics     = "receipt-store.enable-read-write-metrics"
	flagRSLogFilterParallelism = "receipt-store.log-filter-parallelism"
)

// DefaultReceiptLogFilterParallelism is the default per-query block fan-out for
// littidx eth_getLogs (see ReceiptStoreConfig.LogFilterParallelism).
const DefaultReceiptLogFilterParallelism = 16

// ReceiptStoreConfig defines configuration for the receipt store database.
type ReceiptStoreConfig struct {
	// DBDirectory defines the directory to store the receipt store db files
	// If not explicitly set, default to application home directory
	// default to empty
	DBDirectory string `mapstructure:"db-directory"`

	// Backend defines the backend database used for receipt-store.
	// Supported backends: pebbledb (aka pebble)
	// defaults to pebbledb
	Backend string `mapstructure:"rs-backend"`

	// AsyncWriteBuffer defines the async queue length for commits to be applied to receipt store
	// Applies only to the pebbledb backend.
	// Set <= 0 for synchronous writes.
	// defaults to 100
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// KeepRecent defines the number of versions to keep in receipt store.
	// Setting it to 0 means keep everything (no pruning).
	// This is NOT read from receipt-store config; it is always derived from
	// the global min-retain-blocks flag at the app layer.
	KeepRecent int `mapstructure:"-"`

	// PruneIntervalSeconds defines the interval in seconds to trigger pruning
	// default to every 600 seconds
	PruneIntervalSeconds int `mapstructure:"prune-interval-seconds"`

	// ExternalPruning hands retention to the StorageGarbageCollector: the littidx backend stops
	// running its own KeepRecent pruner, and the collector prunes it instead.
	//
	// Like KeepRecent this is not read from the receipt-store config. It is set by whatever
	// constructs the collector, since it is only correct when this store is registered with a
	// running one.
	//
	// Only the littidx backend supports it; newReceiptBackend rejects it on pebbledb.
	ExternalPruning bool `mapstructure:"-"`

	// EnableReadWriteMetrics emits simple estimated read/write counters for Pebble-backed receipt storage.
	// defaults to false
	EnableReadWriteMetrics bool `mapstructure:"enable-read-write-metrics"`

	// LogFilterParallelism bounds how many blocks a single eth_getLogs query
	// scans concurrently in the littidx backend; per-block tag scans and litt
	// body reads are independent, so a range fans across this many workers.
	// Applies only to the littidx backend. <= 0 falls back to the default.
	// defaults to 16
	LogFilterParallelism int `mapstructure:"log-filter-parallelism"`
}

// DefaultReceiptStoreConfig returns the default ReceiptStoreConfig.
// KeepRecent defaults to 0 (no pruning). The app layer is responsible
// for setting KeepRecent from the global min-retain-blocks flag.
func DefaultReceiptStoreConfig() ReceiptStoreConfig {
	return ReceiptStoreConfig{
		Backend:              "pebbledb",
		AsyncWriteBuffer:     DefaultSSAsyncBuffer,
		KeepRecent:           0,
		PruneIntervalSeconds: DefaultSSPruneInterval,
		LogFilterParallelism: DefaultReceiptLogFilterParallelism,
	}
}

// Validate reports whether this configuration is usable.
//
// The backend names a storage implementation, and an unrecognised one leaves the node with no receipt
// store at all. Stated on the type so a diagnostic refuses the same value the boot refuses, rather
// than reporting a file as usable and leaving the refusal for the next restart.
func (c ReceiptStoreConfig) Validate() error {
	_, err := normalizeBackend(c.Backend)
	return err
}

// normalizeBackend returns the canonical spelling of a backend name.
//
// Case and surrounding space are an operator's to get wrong, so they are absorbed here rather than
// refused. An unrecognised name is not, because there is no implementation to fall back to.
func normalizeBackend(raw string) (string, error) {
	backend := strings.ToLower(strings.TrimSpace(raw))
	switch backend {
	case "pebbledb", "pebble", "littidx":
		return backend, nil
	default:
		return "", fmt.Errorf("unsupported receipt-store backend %q; supported: pebbledb, littidx", backend)
	}
}

// ReadReceiptConfig reads receipt store config from app options (e.g. TOML / Viper).
func ReadReceiptConfig(opts AppOptions) (ReceiptStoreConfig, error) {
	cfg := DefaultReceiptStoreConfig()
	if v := opts.Get(flagRSMisnamedBackend); v != nil {
		return cfg, fmt.Errorf("unsupported receipt-store config key %q; use %q instead", flagRSMisnamedBackend, flagRSBackend)
	}
	if v := opts.Get(flagRSDBDirectory); v != nil {
		dbDirectory, err := cast.ToStringE(v)
		if err != nil {
			return cfg, err
		}
		cfg.DBDirectory = strings.TrimSpace(dbDirectory)
	}
	if v := opts.Get(flagRSBackend); v != nil {
		raw, err := cast.ToStringE(v)
		if err != nil {
			return cfg, err
		}
		backend, err := normalizeBackend(raw)
		if err != nil {
			return cfg, err
		}
		cfg.Backend = backend
	}
	if v := opts.Get(flagRSAsyncWriteBuffer); v != nil {
		asyncWriteBuffer, err := cast.ToIntE(v)
		if err != nil {
			return cfg, err
		}
		cfg.AsyncWriteBuffer = asyncWriteBuffer
	}
	if v := opts.Get(flagRSPruneIntervalSeconds); v != nil {
		pruneIntervalSeconds, err := cast.ToIntE(v)
		if err != nil {
			return cfg, err
		}
		cfg.PruneIntervalSeconds = pruneIntervalSeconds
	}
	if v := opts.Get(flagRSReadWriteMetrics); v != nil {
		enableReadWriteMetrics, err := cast.ToBoolE(v)
		if err != nil {
			return cfg, err
		}
		cfg.EnableReadWriteMetrics = enableReadWriteMetrics
	}
	if v := opts.Get(flagRSLogFilterParallelism); v != nil {
		logFilterParallelism, err := cast.ToIntE(v)
		if err != nil {
			return cfg, err
		}
		cfg.LogFilterParallelism = logFilterParallelism
	}
	return cfg, nil
}
