package gc

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
//
// There are two dimensions of garbage collection: snapshots, and history. Snapshots are a mechanism
// utilized to roll back the chain. History is what lets data stores answer queries about historical
// blocks. Not all stores have a concept of "history" (e.g. SC), and not all stores have "snapshots"
// (e.g. the state WAL). A store implements both methods regardless, returning nil from the dimension
// it does not have.
type PrunableStore interface {
	// Name identifies the store in logs and errors. Duplicate names are allowed.
	Name() string

	// PruneHistory may drop the per-block history below blockNumber, and may do so asynchronously.
	// A store that keeps no history returns nil.
	//
	// blockNumber may fall outside the range this store holds, including above its own head. Such a
	// request must be clamped to a no-op rather than emptying the store.
	//
	// Only called when ExternalPruning reports true.
	PruneHistory(blockNumber uint64) error

	// PruneSnapshots may drop every snapshot strictly below blockNumber. A store that keeps no
	// snapshots returns nil.
	//
	// blockNumber is never 0, and never above the block this store last returned from
	// GetRollbackFloor.
	//
	// Only called when ExternalPruning reports true.
	PruneSnapshots(blockNumber uint64) error

	// ExternalPruning reports whether this store's retention is the collector's to enforce:
	//
	//	true  → the collector prunes it, and any pruner inside the store stands down
	//	false → the store prunes itself, and receives neither PruneHistory nor PruneSnapshots
	//
	// A store with no pruner of its own returns true unconditionally.
	ExternalPruning() bool

	// GetRollbackFloor returns the earliest block a rollback of rollbackWindow blocks may target.
	//
	// A store that keeps no snapshots returns head - rollbackWindow, every block in that range being
	// restorable directly. A store that keeps snapshots returns the block number of its highest
	// snapshot at or below head - rollbackWindow, or its lowest snapshot when every snapshot is
	// above that block.
	//
	// It returns 0 — keep everything from block 0 up — when rollbackWindow is deeper than the
	// store's own history, when a snapshot store holds no snapshot to name, or when the store cannot
	// determine what it holds. A store must never return a block above what it can restore to;
	// nothing clamps this answer.
	GetRollbackFloor(rollbackWindow uint64) uint64

	// GetLatestBlock returns the highest block this store has ingested, 0 when it has ingested
	// nothing. It is the head GetRollbackFloor measures rollbackWindow against.
	GetLatestBlock() (uint64, error)
}
