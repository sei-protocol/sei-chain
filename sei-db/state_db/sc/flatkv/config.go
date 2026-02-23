package flatkv

const (
	// DefaultSnapshotInterval matches memiavl's default (10 000 blocks).
	DefaultSnapshotInterval uint32 = 10000
)

// Config defines configuration for the FlatKV (EVM) commit store.
type Config struct {
	// Fsync controls whether data DB writes use fsync for durability.
	// When true (default): all data DB writes use Sync=true for maximum durability.
	// When false: data DBs use Sync=false for better performance.
	// metaDB always uses sync writes regardless of this setting.
	// WAL uses NoSync (matching memiavl); crash recovery relies on Tendermint replay.
	// Default: true
	Fsync bool `mapstructure:"fsync"`

	// AsyncWriteBuffer defines the size of the async write buffer for data DBs.
	// Set <= 0 for synchronous writes.
	// Default: 0 (synchronous)
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// SnapshotInterval defines how often (in blocks) a PebbleDB checkpoint
	// snapshot is taken. 0 disables auto-snapshots.
	// Without periodic snapshots the WAL grows unbounded and every restart
	// replays the entire history from snapshot-0.
	// Default: 10000
	SnapshotInterval uint32 `mapstructure:"snapshot-interval"`
}

// DefaultConfig returns Config with safe default values.
func DefaultConfig() Config {
	return Config{
		Fsync:            true,
		AsyncWriteBuffer: 0,
		SnapshotInterval: DefaultSnapshotInterval,
	}
}
