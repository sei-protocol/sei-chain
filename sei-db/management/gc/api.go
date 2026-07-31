package gc

// InfiniteRetentionWindow is the conventional GetRetentionWindow value meaning "never
// prune this store." Any negative value is treated the same: the store does not vote
// and is never passed to PruneBelow.
//
// Note: elsewhere in this repo (KeepRecent / MinRetainBlocks) 0 often means "keep
// forever." Here 0 means "no extra beyond the shared RollbackWindow," so infinite
// retention needs a negative sentinel.
const InfiniteRetentionWindow int64 = -1

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
type PrunableStore interface {
	// Name identifies the store (logging / errors). Duplicate names are allowed;
	// the collector keys answers by index, not by name.
	Name() string

	// PruneBelow may drop data for all blocks below blockNumber.
	// The store may perform the deletion asynchronously.
	PruneBelow(blockNumber uint64) error

	// GetRetentionWindow is how many extra blocks beyond the shared RollbackWindow
	// this store needs to keep servable. Cut line (head = min non-zero GetLastestBlock):
	//
	//	retention < 0  → store ignored (cutLine treated as 0; conventionally -1)
	//	retention >= 0  → cutLine = head - RollbackWindow - retention
	//
	// RollbackWindow is shared so every store stays consistent under rollback.
	// Retention is optional history on top — kept so the store can still serve queries
	// after a rollback that consumes the entire rollback window.
	//
	// Contract by store kind:
	//   - Contiguous (blockDB, receiptDB, state WAL): -1 / 0 / positive as configured.
	//   - SC: always 0 (only needs the shared rollback window for snapshots).
	//   - SS: always 0 (SS prunes its own version history via KeepRecent; the collector
	//     only drives SS snapshot pruning for the shared rollback window).
	GetRetentionWindow() int64

	// GetPruningBoundary returns the oldest block this store must keep to remain able to
	// serve cutLine, or 0 to opt out of this cycle (not participate in calculating min prune height).
	//
	// Contiguous stores can restore to any height they hold → return cutLine.
	//
	// Snapshot stores can restore only at snapshot heights → return the newest completed
	// snapshot ≤ cutLine, or the oldest completed snapshot if every snapshot is above
	// cutLine (none can be dropped yet). No completed snapshot → 0.
	//
	// Assumption: snapshot creation finishes quickly enough that an in-flight write need
	// not be reserved; modeling long-running snapshot writes is out of scope.
	GetPruningBoundary(cutLine uint64) uint64

	// GetLastestBlock returns the highest block this store has ingested.
	// 0 means "no data / uninitialized" and is ignored when computing the global head.
	GetLastestBlock() (uint64, error)
}
