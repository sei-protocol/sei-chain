package storagemanager

import "fmt"

// Configures a StorageManager.
type StorageManagerConfig struct {

	// The maximum number of blocks the system should be able to roll back at any point in time.
	// Storage manager ensures that enough data is kept on disk such that the system can always
	// roll back this many blocks.
	//
	// Note that the "always able to rollback" invariant may be broken after a rollback. For example, if we normally
	// require a rollback window of 10,000 blocks and then we rollback 5,000 blocks, we can then only rollback an
	// additional 5,000 blocks after that first rollback.
	RollbackWindow uint64

	// The frequency of prune operations, in seconds.
	PruneIntervalSeconds uint64
}

// Construct a default storage manager config.
func DefaultStorageManagerConfig() *StorageManagerConfig {
	return &StorageManagerConfig{
		RollbackWindow:       10_000,
		PruneIntervalSeconds: 60,
	}
}

// Validate the storage manager's config.
func (c *StorageManagerConfig) Validate() error {
	// A zero rollback window is legal: it means the system prunes as aggressively as possible.
	if c.PruneIntervalSeconds == 0 {
		return fmt.Errorf("prune interval must be greater than 0 seconds")
	}
	return nil
}
