package storagemanager

// A store that is persisted via snapshots (e.g. SC, SS).
type SnapshotStore interface {

	// Fetch a list of all block numbers that this store has snapshots for, in ascending sorted order.
	GetStoredBlocks() ([]uint64, error)

	// Instruct the store that it may drop snapshots for all blocks below a specified number.
	// Store may drop data asynchronously.
	PruneBelow(blockNumber uint64) error

	// Return the name of the snapshot store.
	Name() string
}
