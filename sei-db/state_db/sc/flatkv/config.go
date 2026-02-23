package flatkv

import "time"

const (
	DefaultSnapshotInterval        uint32 = 10000
	DefaultSnapshotKeepRecent      uint32 = 1
	DefaultSnapshotMinTimeInterval uint32 = 3600 // seconds
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

	// SnapshotKeepRecent defines how many old snapshots to keep besides the
	// latest one. 0 means keep only the current snapshot (no old snapshots).
	// Default: 1
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// SnapshotMinTimeInterval is the minimum wall-clock seconds between
	// consecutive auto-snapshots. Prevents dense snapshots during catch-up.
	// Default: 3600 (1 hour)
	SnapshotMinTimeInterval uint32 `mapstructure:"snapshot-min-time-interval"`
}

// SnapshotMinTimeDuration converts SnapshotMinTimeInterval to time.Duration.
func (c Config) SnapshotMinTimeDuration() time.Duration {
	return time.Duration(c.SnapshotMinTimeInterval) * time.Second
}

// DefaultConfig returns Config with safe default values.
func DefaultConfig() Config {
	return Config{
		Fsync:                   true,
		AsyncWriteBuffer:        0,
		SnapshotInterval:        DefaultSnapshotInterval,
		SnapshotKeepRecent:      DefaultSnapshotKeepRecent,
		SnapshotMinTimeInterval: DefaultSnapshotMinTimeInterval,
	}
}
