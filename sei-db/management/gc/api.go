package gc

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
//
// Deletion is split into snapshots and per-block history, which are pruned to different depths.
// Every store implements both halves; one that holds no data of a kind returns nil from that half.
type PrunableStore interface {
	// Name identifies the store in logs and errors. Duplicate names are allowed.
	Name() string

	// PruneHistory may drop the per-block history below blockNumber, and may do so
	// asynchronously. Only called when ExternalPruning reports true.
	//
	// blockNumber may fall outside what this store holds, including above its own head. The store
	// must clamp such a request to a no-op rather than empty itself.
	PruneHistory(blockNumber uint64) error

	// PruneSnapshots may drop every snapshot strictly below blockNumber. Only called when
	// ExternalPruning reports true, and never with 0. A store that keeps no snapshots returns nil.
	//
	// blockNumber never exceeds this store's own GetRollbackFloor, so a snapshot it reported is
	// never a candidate for deletion here.
	PruneSnapshots(blockNumber uint64) error

	// ExternalPruning reports whether this store's retention is the collector's to enforce:
	//
	//	true  → the collector prunes it, and any pruner inside the store stands down
	//	false → the store prunes itself, and receives neither PruneHistory nor PruneSnapshots
	//
	// A store with no pruner of its own returns true unconditionally.
	ExternalPruning() bool

	// GetRollbackFloor returns the earliest height a rollback may target given rollbackWindow,
	// measured against this store's own head:
	//
	//	contiguous store → head - rollbackWindow
	//	snapshot store   → the newest snapshot at or below head - rollbackWindow, or its oldest
	//	                   snapshot when every one of them is above that height
	//
	// It returns 0 — keep everything from block 0 up — when the window is deeper than the store's
	// history, when the store has ingested nothing, or when it cannot determine what it holds. The
	// collector derives its cut lines from these answers without capping them, so a store must
	// never report a floor above what it can restore to.
	GetRollbackFloor(rollbackWindow uint64) uint64

	// GetLatestBlock returns the highest block this store has ingested, 0 when it has ingested
	// nothing. It is the head GetRollbackFloor measures rollbackWindow against.
	GetLatestBlock() (uint64, error)
}
