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
	// 0 is allowed: cutLine then equals head for any store with retention 0. That is
	// safe only when every participating store still leaves enough history on its own
	// (e.g. positive GetRetentionWindow, or snapshot votes that keep contiguous stores
	// above genesis). With retention-0 contiguous stores and a snapshot store that
	// answers 0 (no snapshot yet), pruneHeight can reach head and drop everything
	// below it — a snapshot landing near head then cannot be replayed forward.
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
	if c.PruneInterval <= 0 {
		return fmt.Errorf("prune interval must be greater than 0")
	}
	return nil
}
