package gc

import (
	"fmt"
	"time"
)

// StorageGarbageCollectorConfig configures a StorageGarbageCollector.
type StorageGarbageCollectorConfig struct {
	// RollbackWindow is how many blocks behind head the system must remain able to roll back to.
	//
	// 0 waives the guarantee: each store then reports its own head as the earliest height a
	// rollback may target, or its newest snapshot if it keeps snapshots.
	RollbackWindow uint64

	// LookbackWindow is how much queryable history is kept below the rollback window, in blocks.
	// It is extra on top of RollbackWindow rather than a total that includes it, so at least
	// LookbackWindow blocks stay readable below wherever a rollback lands.
	//
	// -1 means infinite: history is never pruned, though snapshots below the rollback floor are
	// still reclaimed. Every other value is a block count and must be >= 0.
	LookbackWindow int64

	// PruneInterval is how often the collector runs a prune cycle. Must be > 0.
	PruneInterval time.Duration
}

// DefaultStorageGarbageCollectorConfig returns the default collector config.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow: 1_000,
		LookbackWindow: 0,
		PruneInterval:  5 * time.Minute,
	}
}

// Validate checks that required fields are set to usable values. LookbackWindow must be >= 0, or -1
// for infinite retention.
func (c *StorageGarbageCollectorConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.PruneInterval <= 0 {
		return fmt.Errorf("prune interval must be greater than 0")
	}
	if c.LookbackWindow < -1 {
		return fmt.Errorf("lookback window must be >= 0, or -1 for infinite retention (got %d)", c.LookbackWindow)
	}
	return nil
}
