package flatkv

// Config defines configuration for the FlatKV (EVM) commit store.
type Config struct {
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

// DefaultConfig returns Config with safe default values.
func DefaultConfig() Config {
	return Config{
		Fsync:            false,
		AsyncWriteBuffer: 0,
	}
}
