package config

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// CheckpointConfig configures a CheckpointScheduler: how far apart the heights it picks are.
//
// A height has to clear every interval set above 0, so with both set the tighter one paces the
// cadence. A value of 0 or less is unused, and with neither set checkpointing is off.
type CheckpointConfig struct {
	// TimeInterval is the wall-clock gap between checkpoints, measured from the last one completing.
	TimeInterval time.Duration

	// BlockInterval places checkpoints on multiples of itself: at 1000, heights 1000, 2000, 3000
	// and so on are eligible.
	BlockInterval int64
}

// Enabled reports whether any interval is set, and so whether a scheduler built from this config
// picks heights at all.
func (c CheckpointConfig) Enabled() bool {
	return c.TimeInterval > 0 || c.BlockInterval > 0
}

// DefaultCheckpointConfig returns a cadence mirroring the state-commit snapshot settings: a
// checkpoint every 10,000 blocks, and no more than one an hour.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		TimeInterval:  time.Duration(memiavl.DefaultSnapshotMinTimeInterval) * time.Second,
		BlockInterval: memiavl.DefaultSnapshotInterval,
	}
}
