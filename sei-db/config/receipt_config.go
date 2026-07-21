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
	flagRSTxIndexBackend       = "receipt-store.tx-index-backend"

	ReceiptTxIndexBackendNone   = ""
	ReceiptTxIndexBackendPebble = "pebbledb"

	// Receipt store backend names. Single source of truth consumed by the
	// receipt package's backend routing and by benchmark configs.
	ReceiptBackendPebble    = "pebbledb"
	ReceiptBackendParquet   = "parquet"
	ReceiptBackendLittDB    = "littdb"
	ReceiptBackendPebbleV3  = "pebblev3"
	ReceiptBackendPebbleIdx = "pebbleidx"
	ReceiptBackendLittIdx   = "littidx"
)

func NormalizeReceiptTxIndexBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "pebbledb":
		return ReceiptTxIndexBackendPebble
	default:
		return ReceiptTxIndexBackendNone
	}
}

// ReceiptStoreConfig defines configuration for the receipt store database.
type ReceiptStoreConfig struct {
	// DBDirectory defines the directory to store the receipt store db files
	// If not explicitly set, default to application home directory
	// default to empty
	DBDirectory string `mapstructure:"db-directory"`

	// LittPaths optionally spreads the litt-backed receipt bodies (littdb /
	// littidx backends) across multiple directories, one per drive; litt
	// shards segment data across them natively. When empty, bodies live at
	// DBDirectory/littdb. Note litt roots every segment's key file on the
	// first path, so give it the roomiest drive.
	LittPaths []string `mapstructure:"litt-paths"`

	// LittKeymapDirectory optionally roots the litt keymap (a pebble DB
	// mapping every live tx hash to its segment address) on its own drive.
	// When empty, the keymap lives under the first litt path.
	LittKeymapDirectory string `mapstructure:"litt-keymap-directory"`

	// LogIndexDirectory optionally relocates the log-filtering index of the
	// littdb/littidx backends (per-block blooms or per-tag keys) onto its own
	// drive. When empty, it lives at DBDirectory/log-index.
	LogIndexDirectory string `mapstructure:"log-index-directory"`

	// Backend defines the backend database used for receipt-store.
	// Supported backends:
	//   pebbledb (aka pebble) - tx-hash-keyed MVCC pebble store (no range queries)
	//   parquet               - rotating parquet files + DuckDB reader
	//   littdb                - LittDB segments (tx hashes as secondary keys)
	//                           plus a small pebble index for log blooms
	//   pebblev3              - block-ordered pebble store (hash index,
	//                           per-block blooms, inline receipt values)
	//   pebbleidx             - pebblev3 with an exact per-tag lookup index
	//                           (block/tag/tx keys) instead of blooms
	//   littidx               - littdb bodies with the exact per-tag pebble
	//                           index instead of blooms (litt for point
	//                           lookups, pebble tags for getLogs)
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

	// TxIndexBackend selects the tx-hash index implementation used by the
	// parquet receipt store. Set to "pebbledb" (the default) to maintain a
	// Pebble-backed tx_hash -> block_number index alongside parquet files so
	// receipt-by-hash lookups can target a single file instead of scanning all
	// files. Set to "" to disable the index; receipt-by-hash lookups that miss
	// the in-memory cache then fail (no full-parquet scan). Ignored when the
	// receipt backend is not parquet.
	TxIndexBackend string `mapstructure:"tx-index-backend"`
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
		TxIndexBackend:       ReceiptTxIndexBackendPebble,
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
		backend, err := cast.ToStringE(v)
		if err != nil {
			return cfg, err
		}
		backend = strings.ToLower(strings.TrimSpace(backend))
		switch backend {
		case ReceiptBackendPebble, "pebble", ReceiptBackendParquet, ReceiptBackendLittDB, ReceiptBackendPebbleV3, ReceiptBackendPebbleIdx, ReceiptBackendLittIdx:
			cfg.Backend = backend
		default:
			return cfg, fmt.Errorf("unsupported receipt-store backend %q; supported: pebbledb, parquet, littdb, pebblev3, pebbleidx, littidx", backend)
		}
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
	if v := opts.Get(flagRSTxIndexBackend); v != nil {
		txIndexBackend, err := cast.ToStringE(v)
		if err != nil {
			return cfg, err
		}
		cfg.TxIndexBackend = NormalizeReceiptTxIndexBackend(txIndexBackend)
	}
	return cfg, nil
}
