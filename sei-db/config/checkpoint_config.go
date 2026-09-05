package config

import "time"

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

// DefaultCheckpointConfig returns a checkpoint no more often than every 10 minutes, taken at
// whatever height first clears that gap. No block interval is set, so no height is refused for
// landing off a boundary.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		TimeInterval:  10 * time.Minute,
		BlockInterval: 0,
	}
}
