package gc

import (
	"fmt"
	"time"
)

// Configures a StorageGarbageCollector.
type StorageGarbageCollectorConfig struct {

	// The maximum number of blocks the system should be able to roll back at any point in time.
	// Storage garbage collector ensures that enough data is kept on disk such that the system can always
	// roll back this many blocks.
	//
	// Note that the "always able to rollback" invariant may be broken after a rollback. For example, if we normally
	// require a rollback window of 10,000 blocks and then we rollback 5,000 blocks, we can then only rollback an
	// additional 5,000 blocks after that first rollback.
	RollbackWindow uint64

	// Additional blocks to retain beyond the rollback window, applied to every store.
	//
	// This is retention the operator wants rather than retention correctness requires: RollbackWindow is what the
	// rollback invariant depends on, while StoreRetention buys history that is served long after it is needed for
	// rollback, such as queries against old blocks and receipts. The two are added together to form the cut line.
	//
	// TODO: make this per-store if the shared value proves too blunt. Because every store is pruned to the lowest
	// block any single store still needs, retention bought for one store is paid for by all of them: raising this to
	// keep more block and receipt history also holds that much extra state in the snapshotted stores, which is far
	// more expensive per block. Separating them means giving the collector a retention value alongside each store
	// (e.g. a []ManagedStore{Store, RetentionBlocks} in place of []PrunableStore) and computing one cut line per
	// store. Note that this only reduces retention where a store's own answer is what bounds the minimum; the
	// contiguous stores must still cover the oldest snapshot anyone kept, or the snapshot becomes unreplayable.
	StoreRetention uint64

	// How often the collector drives a prune cycle.
	PruneInterval time.Duration
}

// Construct a default storage garbage collector config.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow: 1000,
		StoreRetention: 100_000,
		PruneInterval:  10 * time.Minute,
	}
}

// Validate the storage garbage collector's config.
func (c *StorageGarbageCollectorConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	// A zero rollback window is legal: it means the system prunes as aggressively as possible.
	if c.PruneInterval <= 0 {
		return fmt.Errorf("prune interval must be greater than 0")
	}
	// The cut line is derived by subtracting RollbackWindow + StoreRetention from the head of the chain. Reject a pair
	// that overflows when summed, so that subtraction cannot wrap around to a cut line above the head.
	if c.RollbackWindow+c.StoreRetention < c.RollbackWindow {
		return fmt.Errorf("rollback window (%d) plus store retention (%d) overflows uint64",
			c.RollbackWindow, c.StoreRetention)
	}
	return nil
}
