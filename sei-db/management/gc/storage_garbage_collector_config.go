package gc

import (
	"fmt"
	"time"
)

// InfiniteRetentionBeyondRollbackWindow is the conventional RetentionBeyondRollbackWindow
// value for never pruning. Any negative value is treated the same: the collector derives a
// cut line of 0 and skips every prune cycle.
//
// Unlike KeepRecent / MinRetainBlocks elsewhere in this repo (where 0 means keep forever),
// 0 here means "no historical blocks beyond the rollback window" — so infinite needs a
// negative sentinel.
const InfiniteRetentionBeyondRollbackWindow int64 = -1

// Configures a StorageGarbageCollector.
type StorageGarbageCollectorConfig struct {

	// The maximum number of blocks the system should be able to roll back at any point in time.
	// Storage garbage collector ensures that enough data is kept on disk such that the system can always
	// roll back this many blocks. Must be greater than 0: a zero window would let cutLine reach the
	// chain head, and a store with no completed snapshot answering 0 would then leave contiguous
	// stores free to drop everything below head — a snapshot landing at ~head could no longer be
	// replayed forward.
	//
	// Note that the "always able to rollback" invariant may be broken after a rollback. For example, if we normally
	// require a rollback window of 10,000 blocks and then we rollback 5,000 blocks, we can then only rollback an
	// additional 5,000 blocks after that first rollback.
	RollbackWindow uint64

	// How many historical blocks beyond the rollback window the collector guarantees to keep
	// servable, applied to every store. RollbackWindow is what correctness requires for rollback;
	// this field is the additional history operators want for queries against old blocks and
	// receipts. When non-negative, the cut line is
	// head - (RollbackWindow + RetentionBeyondRollbackWindow).
	//
	// Zero means guarantee only the rollback window (no extra history). Any negative value means
	// infinite retention: never prune. Prefer InfiniteRetentionBeyondRollbackWindow (-1) as the
	// conventional sentinel. This differs from KeepRecent / MinRetainBlocks, where 0 means keep
	// forever.
	//
	// TODO: make this per-store if the shared value proves too blunt. Because every store is pruned to the lowest
	// block any single store still needs, retention bought for one store is paid for by all of them: raising this to
	// keep more block and receipt history also holds that much extra state in the snapshotted stores, which is far
	// more expensive per block. Separating them means giving the collector a retention value alongside each store
	// (e.g. a []ManagedStore{Store, RetentionBlocks} in place of []PrunableStore) and computing one cut line per
	// store. Note that this only reduces retention where a store's own answer is what bounds the minimum; the
	// contiguous stores must still cover the oldest snapshot anyone kept, or the snapshot becomes unreplayable.
	RetentionBeyondRollbackWindow int64

	// How often the collector drives a prune cycle.
	PruneInterval time.Duration
}

// Construct a default storage garbage collector config.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow:                1000,
		RetentionBeyondRollbackWindow: 100_000,
		PruneInterval:                 10 * time.Minute,
	}
}

// Validate the storage garbage collector's config.
func (c *StorageGarbageCollectorConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	// RollbackWindow must leave at least one block of margin below head. With RollbackWindow == 0 and
	// RetentionBeyondRollbackWindow == 0, cutLine == globalLatestBlock: contiguous stores may drop
	// everything below head, and a store that answers 0 because it has no completed snapshot yet offers
	// no counter-vote — a snapshot that then lands at ~head cannot be replayed forward. Infinite
	// retention is any negative RetentionBeyondRollbackWindow, not a zero rollback window.
	if c.RollbackWindow == 0 {
		return fmt.Errorf("rollback window must be greater than 0")
	}
	if c.PruneInterval <= 0 {
		return fmt.Errorf("prune interval must be greater than 0")
	}
	// The cut line subtracts RollbackWindow + RetentionBeyondRollbackWindow from the head. Reject a
	// finite pair that overflows when summed, so that subtraction cannot wrap around to a cut line
	// above the head. Negatives mean infinite retention and skip this sum.
	if c.RetentionBeyondRollbackWindow >= 0 {
		retention := uint64(c.RetentionBeyondRollbackWindow)
		if c.RollbackWindow+retention < c.RollbackWindow {
			return fmt.Errorf("rollback window (%d) plus retention beyond rollback window (%d) overflows uint64",
				c.RollbackWindow, c.RetentionBeyondRollbackWindow)
		}
	}
	return nil
}
