package config

// FlatKVConfig defines configuration for the FlatKV (EVM) commit store.
type FlatKVConfig struct {
	// Fsync controls whether data DB writes use fsync for durability.
	// When true (default): all data DB writes use Sync=true for maximum durability.
	// When false: data DBs use Sync=false for better performance.
	// WAL and metaDB always use sync writes regardless of this setting.
	// Default: true
	Fsync bool `mapstructure:"fsync"`

	// AsyncWriteBuffer defines the size of the async write buffer for data DBs.
	// Set <= 0 for synchronous writes.
	// Default: 0 (synchronous)
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`
}

// DefaultFlatKVConfig returns FlatKVConfig with safe default values.
func DefaultFlatKVConfig() FlatKVConfig {
	return FlatKVConfig{
		Fsync:            true,
		AsyncWriteBuffer: 0,
	}
}
