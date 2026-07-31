package gc

import (
	"fmt"
	"time"
)

// StorageGarbageCollectorConfig configures a StorageGarbageCollector.
type StorageGarbageCollectorConfig struct {
	// RollbackWindow is how many blocks behind head the system must remain able to
	// roll back to. Shared by every managed store so they stay mutually consistent;
	// per-store extras beyond this window use PrunableStore.GetRetentionWindow.
	//
	// Must be > 0. With RollbackWindow == 0, cutLine can equal head: contiguous stores
	// may drop everything below head, and a store answering 0 (no snapshot yet) offers
	// no counter-vote — a snapshot that then lands near head cannot be replayed forward.
	//
	// After a rollback that consumes part of the window, full headroom is not promised
	// (e.g. window 10_000, roll back 5_000 → only ~5_000 of headroom remain).
	RollbackWindow uint64

	// PruneInterval is how often the collector runs a prune cycle. Must be > 0.
	PruneInterval time.Duration
}

// DefaultStorageGarbageCollectorConfig returns the default collector config.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow: 1000,
		PruneInterval:  10 * time.Minute,
	}
}

// Validate checks that required fields are set to usable values.
func (c *StorageGarbageCollectorConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.RollbackWindow == 0 {
		return fmt.Errorf("rollback window must be greater than 0")
	}
	if c.PruneInterval <= 0 {
		return fmt.Errorf("prune interval must be greater than 0")
	}
	return nil
}
