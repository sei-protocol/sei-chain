package gc

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
type PrunableStore interface {
	// Name Returns the name of the store.
	Name() string

	// PruneBelow tells the store that it may drop snapshots/data for all blocks below a specified number.
	// Store can drop data asynchronously in the background.
	PruneBelow(blockNumber uint64) error

	// GetOldestBlockToRetain returns the oldest block this store must keep in order to remain able to serve cutLine,
	// or 0 when the store opts out of pruning entirely (e.g. infinite retention or disabled).
	//
	// The collector ignores a store that answers 0 when choosing the prune height and does not call PruneBelow on it,
	// so that other stores can still be pruned.
	//
	// A store that retains a contiguous range of blocks (blockDB, receiptDB, the state WAL) can be restored to any
	// block it holds, so it should simply return the cutLine unchanged.
	//
	// A store with checkpoints can only be restored to a height it has a snapshot for, so it answers the newest
	// completed snapshot at or below cutLine, or its oldest completed snapshot when every snapshot is above the cut
	// line, since in that case none of them can be dropped. With no completed snapshot at all it answers 0.
	//
	// Snapshot creation is assumed to complete within seconds, so a snapshot write in flight is not modeled: a store
	// need not reserve blocks for a snapshot that has not landed yet. A store whose snapshots take long enough for the
	// cut line to advance past them would need to report the in-progress height instead, and that is out of scope here.
	GetOldestBlockToRetain(cutLine uint64) uint64

	// GetLastCommittedBlock returns the highest block this store has ingested.
	GetLastCommittedBlock() (uint64, error)
}
