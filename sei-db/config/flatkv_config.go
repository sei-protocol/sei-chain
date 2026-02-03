package config

// FlatKVConfig defines configuration for the FlatKV (EVM) commit store.
type FlatKVConfig struct {
	// EnableStorageWrites enables writes to storageDB and its LtHash contribution.
	// When false, storage data is skipped entirely (no DB writes, no LtHash updates).
	// Default: true
	EnableStorageWrites bool `mapstructure:"enable-storage-writes"`

	// EnableAccountWrites enables writes to accountDB and its LtHash contribution.
	// When false, account data is skipped entirely (no DB writes, no LtHash updates).
	// Default: true
	EnableAccountWrites bool `mapstructure:"enable-account-writes"`

	// EnableCodeWrites enables writes to codeDB and its LtHash contribution.
	// When false, code data is skipped entirely (no DB writes, no LtHash updates).
	// Default: true
	EnableCodeWrites bool `mapstructure:"enable-code-writes"`

	// AsyncWrites enables async writes to data DBs for better performance.
	// When true: data DBs use Sync=false, then Flush() at FlushInterval.
	// When false (default): all writes use Sync=true for maximum durability.
	// WAL and metaDB always use sync writes regardless of this setting.
	// Default: false
	AsyncWrites bool `mapstructure:"async-writes"`

	// FlushInterval controls how often to flush data DBs and update metaDB.
	// Only applies when AsyncWrites=true.
	// - 0 or 1: flush every block (safest, slowest)
	// - N > 1: flush every N blocks (faster, recovers up to N blocks from WAL)
	// Default: 100
	FlushInterval int `mapstructure:"flush-interval"`
}

// DefaultFlatKVConfig returns FlatKVConfig with default values.
func DefaultFlatKVConfig() FlatKVConfig {
	return FlatKVConfig{
		EnableStorageWrites: true,
		EnableAccountWrites: true,
		EnableCodeWrites:    true,
		AsyncWrites:         false,
		FlushInterval:       100,
	}
}
