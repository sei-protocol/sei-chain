package gc

import (
	"fmt"
	"time"
)

// StorageGarbageCollectorConfig configures a StorageGarbageCollector.
type StorageGarbageCollectorConfig struct {
	// RollbackWindow is how many blocks behind head the system must remain able to roll back
	// to. It buys the ability to rewind; LookbackWindow buys the ability to still read history
	// afterwards, and the two are separate knobs because they answer to different needs.
	//
	// 0 is allowed and waives the guarantee: each store then reports its own head as the
	// earliest height a rollback may target, and a snapshot store reports its newest snapshot.
	RollbackWindow uint64

	// LookbackWindow is how much queryable history is kept behind the rollback window, in blocks.
	// It is extra on top of RollbackWindow rather than a total that includes it, which is what
	// makes the promise independent of rollback depth: however far the node rewinds inside the
	// rollback window, at least LookbackWindow blocks below the new head stay readable.
	//
	// One window covers every managed store. With F = LatestBlock - RollbackWindow -
	// LookbackWindow, collection guarantees:
	//
	//  1. Nothing needed to roll back to any block in
	//     [LatestBlock - RollbackWindow, LatestBlock] is deleted.
	//  2. No data at or above F is deleted.
	//  3. Data below F is eventually deleted, on each store's own reclamation schedule.
	//
	// Snapshots stop one window short of history, at the lowest rollback floor the stores report,
	// since they are restore points and this window buys history to read rather than history to
	// restore to. History goes LookbackWindow below that same floor, because a retained snapshot
	// is only restorable if the blocks above it survive. See PrunableStore.
	LookbackWindow uint64

	// PruneInterval is how often the collector runs a prune cycle. Must be > 0.
	PruneInterval time.Duration
}

// DefaultStorageGarbageCollectorConfig returns the default collector config.
//
// LookbackWindow is 0: extra history costs disk on every managed store at once, so it belongs at
// the call site where that cost is visible rather than in a default.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow: 1_000,
		LookbackWindow: 0,
		PruneInterval:  5 * time.Minute,
	}
}

// Validate checks that required fields are set to usable values.
//
// The windows are additive, so every combination of them is meaningful and neither is constrained
// against the other. A sum that overflows uint64 is handled in getHistoryCutLine.
func (c *StorageGarbageCollectorConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.PruneInterval <= 0 {
		return fmt.Errorf("prune interval must be greater than 0")
	}
	return nil
}
