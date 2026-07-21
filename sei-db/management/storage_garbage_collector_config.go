package management

import (
	"fmt"
	"math"
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

	// The frequency of prune operations, in seconds.
	PruneIntervalSeconds uint64
}

// Construct a default storage garbage collector config.
func DefaultStorageGarbageCollectorConfig() *StorageGarbageCollectorConfig {
	return &StorageGarbageCollectorConfig{
		RollbackWindow:       10_000,
		PruneIntervalSeconds: 60,
	}
}

// Validate the storage garbage collector's config.
func (c *StorageGarbageCollectorConfig) Validate() error {
	// A zero rollback window is legal: it means the system prunes as aggressively as possible.
	if c.PruneIntervalSeconds == 0 {
		return fmt.Errorf("prune interval must be greater than 0 seconds")
	}
	// The prune interval is converted to a time.Duration (int64 nanoseconds) when the ticker is created. Reject values
	// that would overflow that conversion so the ticker can never be handed a zero/negative duration.
	if c.PruneIntervalSeconds > math.MaxInt64/uint64(time.Second) {
		return fmt.Errorf("prune interval must be at most %d seconds", math.MaxInt64/uint64(time.Second))
	}
	return nil
}
