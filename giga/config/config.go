// Package config defines the [giga] section of app.toml: the storage, execution, EVM RPC and
// state sync settings a Giga (Autobahn) node runs with.
package config

import (
	"fmt"
	"time"

	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
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
	RPC       RPCConfig       `mapstructure:"rpc"`
	StateSync StateSyncConfig `mapstructure:"state_sync"`
}

// StorageConfig is the [giga.storage] sub-table: retention and layout of the Giga stores.
type StorageConfig struct {
	// Mode selects the store layout: "validator" skips the state store, "full" opens it for
	// serving queries. Empty follows the node's mode from config.toml.
	Mode string `mapstructure:"mode"`
	// Receipts opens the receipt store, which the node's EVM RPC serves transaction receipts,
	// logs and fee history from. When false the node starts no EVM RPC, and refuses to start while a
	// receipt store with history is on disk: move that directory away first.
	Receipts bool `mapstructure:"receipts"`
	// RollbackWindow is how many blocks behind head the node must remain able to roll back to.
	RollbackWindow uint64 `mapstructure:"rollback_window"`
	// LookbackWindow is how many queryable blocks are kept below the rollback window; -1 keeps all.
	LookbackWindow int64 `mapstructure:"lookback_window"`
	// StateStoreBackend is how the state store keeps its history: "pebbledb" stores every version of
	// every key, "undolog" the value each block replaced, over the current state.
	StateStoreBackend string `mapstructure:"state_store_backend"`
	// PruneInterval is how often the storage garbage collector runs.
	PruneInterval time.Duration `mapstructure:"prune_interval"`
	// CheckpointTimeInterval is the wall-clock gap between state-commit checkpoints.
	CheckpointTimeInterval time.Duration `mapstructure:"checkpoint_time_interval"`
	// CheckpointBlockInterval places checkpoints on multiples of itself; 0 disables the rule.
	CheckpointBlockInterval int64 `mapstructure:"checkpoint_block_interval"`
	// HashVaultFsyncDisabledUnsafe writes the app-hash vault without fsync. A crash can then lose the
	// latest recorded hash, so it is for load tests only.
	HashVaultFsyncDisabledUnsafe bool `mapstructure:"hash_vault_fsync_disabled_unsafe"`
}

// ExecutionConfig is the [giga.execution] sub-table: how the EVM-only executor runs blocks.
type ExecutionConfig struct {
	// MinGasPrice is the lowest effective gas price, in wei, this node admits a transaction at.
	// It only filters local admission; block validity uses the chain-wide floor, and values
	// below it are raised to it.
	MinGasPrice uint64 `mapstructure:"min_gas_price"`
	// OCCWorkers is the number of parallel execution workers; 0 uses GOMAXPROCS.
	OCCWorkers int `mapstructure:"occ_workers"`
	// ParseWorkers is the number of parallel transaction decoding workers; 0 uses GOMAXPROCS.
	ParseWorkers int `mapstructure:"parse_workers"`
	// BlockResultPoolSize is the number of block results kept pooled between executions.
	BlockResultPoolSize int `mapstructure:"block_result_pool_size"`
	// EvmRpcPort is the TCP port the EVM-only JSON-RPC listens on, on all interfaces.
	EvmRpcPort int `mapstructure:"evm_rpc_port"`
	// EvmWsPort is the TCP port the EVM-only JSON-RPC serves WebSocket on, on all interfaces.
	EvmWsPort int `mapstructure:"evm_ws_port"`
	// GCPercent is the Go GC target. 0 leaves the process default. A value above 100 trades memory
	// for fewer collections; the process then caps the heap from the container limit when it has one.
	GCPercent int `mapstructure:"gc_percent"`
}

// RPCConfig is the [giga.rpc] sub-table: the limits the EVM-only JSON-RPC server enforces. A zero
// value disables the limit it names, except where a field says otherwise.
type RPCConfig struct {
	// MaxSubscriptionsNewHead caps open eth_subscribe("newHeads") subscriptions across all
	// connections.
	MaxSubscriptionsNewHead uint64 `mapstructure:"max_subscriptions_new_head"`
	// MaxSubscriptionsPerConnection caps the open subscriptions of a single WebSocket connection.
	MaxSubscriptionsPerConnection uint64 `mapstructure:"max_subscriptions_per_connection"`
	// MaxRequestBodyBytes caps the size, in bytes, of one HTTP request body or WebSocket message.
	// It must be positive.
	MaxRequestBodyBytes int `mapstructure:"max_request_body_bytes"`
	// MaxConcurrentRequestBytes caps the total size, in bytes, of WebSocket request frames read
	// and processed concurrently. Values below MaxRequestBodyBytes act as MaxRequestBodyBytes.
	MaxConcurrentRequestBytes int64 `mapstructure:"max_concurrent_request_bytes"`
	// WSAdmissionTimeout is how long a WebSocket connection waits for MaxConcurrentRequestBytes
	// budget before it is closed; zero uses the go-ethereum default of 30s.
	WSAdmissionTimeout time.Duration `mapstructure:"ws_admission_timeout"`
	// BatchRequestLimit caps the number of requests in one JSON-RPC batch.
	BatchRequestLimit int `mapstructure:"batch_request_limit"`
	// BatchResponseMaxSize caps the total size, in bytes, of one JSON-RPC batch's responses.
	BatchResponseMaxSize int `mapstructure:"batch_response_max_size"`
	// MaxWSConnections caps simultaneously open WebSocket connections.
	MaxWSConnections int `mapstructure:"max_ws_connections"`
}

// StateSyncConfig is the [giga.state_sync] sub-table: whether this node uses giga state sync.
type StateSyncConfig struct {
	// Enabled allows the state-sync-snapshot create and restore commands on this node, and the
	// seed of a restored home on start.
	Enabled bool `mapstructure:"enabled"`
}

// DefaultConfig is what a Giga node runs when the section is absent. Storage defaults are the
// sei-db ones, so there is a single place they are defined.
var DefaultConfig = Config{
	Storage: defaultStorageConfig(),
	Execution: ExecutionConfig{
		MinGasPrice:         1_000_000_000,
		OCCWorkers:          0,
		ParseWorkers:        0,
		BlockResultPoolSize: 1,
		EvmRpcPort:          8545,
		EvmWsPort:           8546,
	},
	// The same limits the v2 EVM RPC defaults to.
	RPC: RPCConfig{
		MaxSubscriptionsNewHead:       10_000,
		MaxSubscriptionsPerConnection: 100,
		MaxRequestBodyBytes:           5 * 1024 * 1024,
		MaxConcurrentRequestBytes:     128 * 1024 * 1024,
		WSAdmissionTimeout:            30 * time.Second,
		BatchRequestLimit:             100,
		BatchResponseMaxSize:          25 * 1000 * 1000,
		MaxWSConnections:              2000,
	},
	StateSync: StateSyncConfig{
		Enabled: false,
	},
}

func defaultStorageConfig() StorageConfig {
	gc := seidbconfig.DefaultStorageGarbageCollectorConfig()
	cp := seidbconfig.DefaultCheckpointConfig()
	return StorageConfig{
		Mode:                    StorageModeAuto,
		Receipts:                true,
		RollbackWindow:          gc.RollbackWindow,
		LookbackWindow:          gc.LookbackWindow,
		StateStoreBackend:       seidbconfig.DefaultSSBackend,
		PruneInterval:           gc.PruneInterval,
		CheckpointTimeInterval:  cp.TimeInterval,
		CheckpointBlockInterval: cp.BlockInterval,
	}
}

// The keys this package's reader resolves.
const (
	FlagStorageMode                      = "giga.storage.mode"
	FlagStorageReceipts                  = "giga.storage.receipts"
	FlagStorageRollbackWindow            = "giga.storage.rollback_window"
	FlagStorageLookbackWindow            = "giga.storage.lookback_window"
	FlagStorageStateStoreBackend         = "giga.storage.state_store_backend"
	FlagStoragePruneInterval             = "giga.storage.prune_interval"
	FlagStorageCheckpointTimeInterval    = "giga.storage.checkpoint_time_interval"
	FlagStorageCheckpointBlockInterval   = "giga.storage.checkpoint_block_interval"
	FlagStorageHashVaultFsyncDisabled    = "giga.storage.hash_vault_fsync_disabled_unsafe"
	FlagExecutionMinGasPrice             = "giga.execution.min_gas_price"
	FlagExecutionOCCWorkers              = "giga.execution.occ_workers"
	FlagExecutionParseWorkers            = "giga.execution.parse_workers"
	FlagExecutionBlockResultPoolSize     = "giga.execution.block_result_pool_size"
	FlagExecutionEvmRpcPort              = "giga.execution.evm_rpc_port"
	FlagExecutionEvmWsPort               = "giga.execution.evm_ws_port"
	FlagExecutionGCPercent               = "giga.execution.gc_percent"
	FlagRPCMaxSubscriptionsNewHead       = "giga.rpc.max_subscriptions_new_head"
	FlagRPCMaxSubscriptionsPerConnection = "giga.rpc.max_subscriptions_per_connection"
	FlagRPCMaxRequestBodyBytes           = "giga.rpc.max_request_body_bytes"
	FlagRPCMaxConcurrentRequestBytes     = "giga.rpc.max_concurrent_request_bytes"
	FlagRPCWSAdmissionTimeout            = "giga.rpc.ws_admission_timeout"
	FlagRPCBatchRequestLimit             = "giga.rpc.batch_request_limit"
	FlagRPCBatchResponseMaxSize          = "giga.rpc.batch_response_max_size"
	FlagRPCMaxWSConnections              = "giga.rpc.max_ws_connections"
	FlagStateSyncEnabled                 = "giga.state_sync.enabled"
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
	if v := opts.Get(FlagStorageReceipts); v != nil {
		if cfg.Storage.Receipts, err = cast.ToBoolE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageReceipts, err)
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
	if v := opts.Get(FlagStorageStateStoreBackend); v != nil {
		if cfg.Storage.StateStoreBackend, err = cast.ToStringE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageStateStoreBackend, err)
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
	if v := opts.Get(FlagStorageHashVaultFsyncDisabled); v != nil {
		if cfg.Storage.HashVaultFsyncDisabledUnsafe, err = cast.ToBoolE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStorageHashVaultFsyncDisabled, err)
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
	if v := opts.Get(FlagExecutionEvmRpcPort); v != nil {
		if cfg.Execution.EvmRpcPort, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionEvmRpcPort, err)
		}
	}
	if v := opts.Get(FlagExecutionEvmWsPort); v != nil {
		if cfg.Execution.EvmWsPort, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionEvmWsPort, err)
		}
	}
	if v := opts.Get(FlagExecutionGCPercent); v != nil {
		if cfg.Execution.GCPercent, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagExecutionGCPercent, err)
		}
	}
	if v := opts.Get(FlagRPCMaxSubscriptionsNewHead); v != nil {
		if cfg.RPC.MaxSubscriptionsNewHead, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCMaxSubscriptionsNewHead, err)
		}
	}
	if v := opts.Get(FlagRPCMaxSubscriptionsPerConnection); v != nil {
		if cfg.RPC.MaxSubscriptionsPerConnection, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCMaxSubscriptionsPerConnection, err)
		}
	}
	if v := opts.Get(FlagRPCMaxRequestBodyBytes); v != nil {
		if cfg.RPC.MaxRequestBodyBytes, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCMaxRequestBodyBytes, err)
		}
	}
	if v := opts.Get(FlagRPCMaxConcurrentRequestBytes); v != nil {
		if cfg.RPC.MaxConcurrentRequestBytes, err = cast.ToInt64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCMaxConcurrentRequestBytes, err)
		}
	}
	if v := opts.Get(FlagRPCWSAdmissionTimeout); v != nil {
		if cfg.RPC.WSAdmissionTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCWSAdmissionTimeout, err)
		}
	}
	if v := opts.Get(FlagRPCBatchRequestLimit); v != nil {
		if cfg.RPC.BatchRequestLimit, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCBatchRequestLimit, err)
		}
	}
	if v := opts.Get(FlagRPCBatchResponseMaxSize); v != nil {
		if cfg.RPC.BatchResponseMaxSize, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCBatchResponseMaxSize, err)
		}
	}
	if v := opts.Get(FlagRPCMaxWSConnections); v != nil {
		if cfg.RPC.MaxWSConnections, err = cast.ToIntE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagRPCMaxWSConnections, err)
		}
	}
	if v := opts.Get(FlagStateSyncEnabled); v != nil {
		if cfg.StateSync.Enabled, err = cast.ToBoolE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", FlagStateSyncEnabled, err)
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
	switch c.Storage.StateStoreBackend {
	case seidbconfig.PebbleDBBackend, seidbconfig.PebbleDBUndoBackend:
	default:
		return fmt.Errorf("%s: %q is not %q or %q", FlagStorageStateStoreBackend, c.Storage.StateStoreBackend,
			seidbconfig.PebbleDBBackend, seidbconfig.PebbleDBUndoBackend)
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
	if c.Execution.GCPercent < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagExecutionGCPercent, c.Execution.GCPercent)
	}
	if c.Execution.BlockResultPoolSize < 1 {
		return fmt.Errorf("%s: must be >= 1, got %d", FlagExecutionBlockResultPoolSize,
			c.Execution.BlockResultPoolSize)
	}
	if c.Execution.EvmRpcPort < 1 || c.Execution.EvmRpcPort > 65535 {
		return fmt.Errorf("%s: must be in 1..65535, got %d", FlagExecutionEvmRpcPort, c.Execution.EvmRpcPort)
	}
	if c.Execution.EvmWsPort < 1 || c.Execution.EvmWsPort > 65535 {
		return fmt.Errorf("%s: must be in 1..65535, got %d", FlagExecutionEvmWsPort, c.Execution.EvmWsPort)
	}
	if c.Execution.EvmWsPort == c.Execution.EvmRpcPort {
		return fmt.Errorf("%s: must differ from %s, both are %d", FlagExecutionEvmWsPort, FlagExecutionEvmRpcPort,
			c.Execution.EvmWsPort)
	}
	if c.RPC.MaxRequestBodyBytes < 1 {
		return fmt.Errorf("%s: must be positive, got %d", FlagRPCMaxRequestBodyBytes, c.RPC.MaxRequestBodyBytes)
	}
	if c.RPC.MaxConcurrentRequestBytes < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagRPCMaxConcurrentRequestBytes, c.RPC.MaxConcurrentRequestBytes)
	}
	if c.RPC.WSAdmissionTimeout < 0 {
		return fmt.Errorf("%s: must be >= 0, got %s", FlagRPCWSAdmissionTimeout, c.RPC.WSAdmissionTimeout)
	}
	if c.RPC.BatchRequestLimit < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagRPCBatchRequestLimit, c.RPC.BatchRequestLimit)
	}
	if c.RPC.BatchResponseMaxSize < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagRPCBatchResponseMaxSize, c.RPC.BatchResponseMaxSize)
	}
	if c.RPC.MaxWSConnections < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", FlagRPCMaxWSConnections, c.RPC.MaxWSConnections)
	}
	return nil
}

// ConfigTemplate is the TOML template for the [giga] section of app.toml.
const ConfigTemplate = `
###############################################################################
###                          Giga Node Configuration                        ###
###############################################################################

# Storage, execution, EVM RPC and state sync settings of a Giga (Autobahn) node. Committee and consensus
# settings stay in the file named by autobahn-config-file in config.toml.

[giga.storage]
# mode selects the store layout. "validator" skips the state store; "full" opens it
# for serving queries. Empty follows the node's mode in config.toml.
mode = "{{ .Giga.Storage.Mode }}"

# receipts opens the receipt store, which the node's EVM RPC serves transaction receipts,
# logs and fee history from. When false the node starts no EVM RPC, and refuses to start while a
# receipt store with history is on disk: move that directory away first.
receipts = {{ .Giga.Storage.Receipts }}

# rollback_window is how many blocks behind head the node must remain able to roll back to.
rollback_window = {{ .Giga.Storage.RollbackWindow }}

# lookback_window is how many queryable blocks are kept below the rollback window.
# -1 keeps all history.
lookback_window = {{ .Giga.Storage.LookbackWindow }}

# state_store_backend is how the state store keeps its history. "pebbledb" stores every version
# of every key; "undolog" stores the value each block replaced, over the current state, and
# drops expired history a bucket at a time.
state_store_backend = "{{ .Giga.Storage.StateStoreBackend }}"

# prune_interval is how often the storage garbage collector runs.
prune_interval = "{{ .Giga.Storage.PruneInterval }}"

# checkpoint_time_interval is the wall-clock gap between state-commit checkpoints.
checkpoint_time_interval = "{{ .Giga.Storage.CheckpointTimeInterval }}"

# checkpoint_block_interval places checkpoints on multiples of itself; 0 disables the rule.
checkpoint_block_interval = {{ .Giga.Storage.CheckpointBlockInterval }}

# hash_vault_fsync_disabled_unsafe writes the app-hash vault without fsync. A crash can then lose the
# latest recorded hash, so it is for load tests only.
hash_vault_fsync_disabled_unsafe = {{ .Giga.Storage.HashVaultFsyncDisabledUnsafe }}

[giga.execution]
# min_gas_price is the lowest effective gas price, in wei, this node admits a transaction at.
# It only filters local admission; block validity uses the chain-wide floor, and values
# below it are raised to it.
min_gas_price = {{ .Giga.Execution.MinGasPrice }}

# occ_workers is the number of parallel execution workers; 0 uses GOMAXPROCS.
occ_workers = {{ .Giga.Execution.OCCWorkers }}

# parse_workers is the number of parallel transaction decoding workers; 0 uses GOMAXPROCS.
parse_workers = {{ .Giga.Execution.ParseWorkers }}

# block_result_pool_size is the number of block results kept pooled between executions.
block_result_pool_size = {{ .Giga.Execution.BlockResultPoolSize }}

# evm_rpc_port is the TCP port the EVM-only JSON-RPC listens on, on all interfaces.
evm_rpc_port = {{ .Giga.Execution.EvmRpcPort }}

# evm_ws_port is the TCP port the EVM-only JSON-RPC serves WebSocket on, on all interfaces.
evm_ws_port = {{ .Giga.Execution.EvmWsPort }}

# gc_percent is the Go GC target. 0 leaves the process default.
gc_percent = {{ .Giga.Execution.GCPercent }}

[giga.rpc]
# Limits the EVM-only JSON-RPC server enforces. 0 disables a limit.

# max_subscriptions_new_head caps open eth_subscribe("newHeads") subscriptions across all
# connections.
max_subscriptions_new_head = {{ .Giga.RPC.MaxSubscriptionsNewHead }}

# max_subscriptions_per_connection caps the open subscriptions of a single WebSocket connection.
max_subscriptions_per_connection = {{ .Giga.RPC.MaxSubscriptionsPerConnection }}

# max_request_body_bytes caps the size, in bytes, of one HTTP request body or WebSocket message.
# It must be positive.
max_request_body_bytes = {{ .Giga.RPC.MaxRequestBodyBytes }}

# max_concurrent_request_bytes caps the total size, in bytes, of WebSocket request frames read
# and processed concurrently. Values below max_request_body_bytes act as max_request_body_bytes.
max_concurrent_request_bytes = {{ .Giga.RPC.MaxConcurrentRequestBytes }}

# ws_admission_timeout is how long a WebSocket connection waits for max_concurrent_request_bytes
# budget before it is closed; 0 uses the go-ethereum default of 30s.
ws_admission_timeout = "{{ .Giga.RPC.WSAdmissionTimeout }}"

# batch_request_limit caps the number of requests in one JSON-RPC batch.
batch_request_limit = {{ .Giga.RPC.BatchRequestLimit }}

# batch_response_max_size caps the total size, in bytes, of one JSON-RPC batch's responses.
batch_response_max_size = {{ .Giga.RPC.BatchResponseMaxSize }}

# max_ws_connections caps simultaneously open WebSocket connections.
max_ws_connections = {{ .Giga.RPC.MaxWSConnections }}

[giga.state_sync]
# enabled allows the state-sync-snapshot create and restore commands on this node, and the
# seed of a restored home on start.
enabled = {{ .Giga.StateSync.Enabled }}
`
