package gc

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
//
// Deletion is split in two because the two halves are pruned to different depths. Snapshots go down
// to the deepest rollback the fleet still owes, since a restore point below that is one nothing can
// ask for. History goes one lookback window deeper, because the floor snapshot is only restorable if
// the blocks above it survive, and because that history is still readable where the snapshot is not.
//
// Every store implements both halves. Most hold only one kind of data and return nil from the
// other, which is a cheaper contract than an optional interface: a store cannot drop out of a
// prune by failing to implement a method, so "this store keeps no snapshots" is a claim written
// down in the store rather than inferred from a type assertion in the collector.
//
// The store, not the collector, turns the rollback window into a height. It is the only party that
// knows what it actually holds — where its snapshots sit, how far its ingest has got — so it reports
// a floor and the collector reduces those to the cut lines it hands back.
type PrunableStore interface {
	// Name identifies the store for logs and errors. Duplicate names are allowed; the
	// collector keys answers by index, not by name.
	Name() string

	// PruneHistory may drop the per-block history below blockNumber, leaving snapshots to
	// PruneSnapshots. The store may perform the deletion asynchronously. Only called when
	// ExternalPruning reports true.
	//
	// blockNumber is calculated with the consideration of both lookback and rollback window.
	// For example, WAL can only prune the block number up till the last snapshot height.
	//
	// It is the minimum across every participating store less the lookback window, so it can sit
	// below what this store alone would need — and above this store's head, since a store may lag
	// the ones that set the minimum. Both are the store's to absorb: a request outside what it
	// holds must clamp to a no-op rather than empty the store.
	PruneHistory(blockNumber uint64) error

	// PruneSnapshots may drop every snapshot strictly below blockNumber, leaving the per-block
	// history to PruneHistory. Only called when ExternalPruning reports true, and never with 0.
	//
	// blockNumber is the minimum of every store's GetRollbackFloor, so it sits at or below what
	// this store itself reported. A store that reported a snapshot therefore always keeps that
	// snapshot: it is at or above this height by construction, and so is never a candidate here.
	// That is what makes the collector's minimum meaningful — history is held at a floor the
	// store still has the restore point for.
	//
	// It stops one lookback window short of where PruneHistory goes, and the asymmetry is the
	// point. Restoring to any height inside the rollback window starts from the floor snapshot
	// and replays history forward, so a snapshot below that floor is a restore point nothing can
	// ask for, while the history below it is still readable and is what the lookback window
	// buys.
	//
	// A store that keeps no snapshots returns nil.
	PruneSnapshots(blockNumber uint64) error

	// ExternalPruning reports whether this store's retention is the collector's to enforce.
	//
	//	true  → the collector prunes it; any pruner inside the store must stand down
	//	false → the store prunes itself; it receives neither PruneHistory nor PruneSnapshots
	//
	// This exists so that "the collector manages this store" and "the store's own pruner is
	// off" are one fact rather than two settings that can disagree. A store with an internal
	// pruner answers from the same field that pruner consults, which is what makes the two
	// mutually exclusive by construction instead of by wiring discipline. A store with no
	// pruner of its own returns true unconditionally.
	ExternalPruning() bool

	// GetRollbackFloor returns the earliest height a rollback may target: given rollbackWindow,
	// the deepest height this store could still be asked to restore to, and so the oldest block
	// it needs the fleet to keep. The collector takes the minimum across stores to get the
	// snapshot cut line, then subtracts LookbackWindow from that for the history cut line.
	//
	// The window is measured against the store's own head rather than a height handed down, so a
	// store ahead of the fleet answers from where it actually is:
	//
	//	contiguous store → head - rollbackWindow, every height in between being restorable
	//	snapshot store   → the newest snapshot at or below head - rollbackWindow, restoring
	//	                   starting there and replaying forward; or its oldest snapshot when
	//	                   every one of them is above that height, that being the deepest it
	//	                   can reach
	//
	// 0 means nothing here is eligible for pruning, and it is a height rather than a sentinel:
	// keep everything from block 0 up. Because both cut lines are derived from the minimum of
	// these answers, one store answering 0 holds the whole fleet where it is for the cycle.
	// Three situations reach it, and all three want that outcome:
	//
	//	head <= rollbackWindow → the promised rollback is deeper than this store's whole
	//	                         history, so no part of it can be given up yet
	//	nothing ingested yet   → the same case with a head of 0
	//	cannot tell what it holds → an unreadable head or snapshot listing; the store must not
	//	                         let history be dropped on the strength of a guess
	//
	// Answering high is the damaging direction, since nothing above clamps it: the collector
	// derives its cut lines from these answers rather than capping them.
	GetRollbackFloor(rollbackWindow uint64) uint64

	// GetLatestBlock returns the highest block this store has ingested, 0 when it has ingested
	// nothing.
	//
	// It is the head GetRollbackFloor measures the rollback window against. The collector does
	// not call it — every height it acts on comes from GetRollbackFloor — so this is here as the
	// store's ingest position for operators and tests.
	GetLatestBlock() (uint64, error)
}
