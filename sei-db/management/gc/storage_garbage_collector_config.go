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
	// 0 is allowed and waives the guarantee: cutLine then equals head for any store with
	// retention 0, so those stores are pruned to their head. It is safe only when every
	// participating store leaves enough history on its own — a positive GetRetentionWindow,
	// or snapshot answers that hold the contiguous stores above genesis. It also disables
	// the CannotServeRollback stop signal, so a store that has not snapshotted yet loses the
	// range it would have replayed from. See that constant.
	//
	// After a rollback that consumes part of the window, full headroom is not promised
	// (e.g. window 10_000, roll back 5_000 → only ~5_000 of headroom remain).
	RollbackWindow uint64

	// PruneInterval is how often the collector runs a prune cycle. Must be > 0.
	PruneInterval time.Duration
}

// DefaultStorageGarbageCollectorConfig returns the default collector config.
//
// Both values are deliberately lower than the pre-unification defaults (RollbackWindow 10_000,
// interval 60s), to match how these are actually used:
//
//   - RollbackWindow 1_000. Real rollbacks are a few blocks deep, so 1_000 is already ample
//     headroom, and this is only the correctness floor. The production shape is a short
//     rollback window paired with much longer history, and that history now belongs on each
//     store via GetRetentionWindow rather than being folded into this shared window — keeping
//     it short here avoids charging every store for retention only some of them need.
//   - PruneInterval 5m. Pruning reclaims whole files lazily; running it every minute buys
//     nothing and costs I/O against a live node. Anything in the 5-10 minute range is fine.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow: 1_000,
		PruneInterval:  5 * time.Minute,
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
