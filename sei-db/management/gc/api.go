package gc

// InfiniteRetentionWindow is the GetRetentionWindow value meaning "never prune this store": its
// cut line is forced to 0, so it is never asked for a boundary and never passed to PruneBelow.
//
// Note that 0 here means "no extra beyond the shared RollbackWindow", not "keep forever" as
// KeepRecent / MinRetainBlocks do elsewhere in this repo — hence the negative sentinel.
const InfiniteRetentionWindow int64 = -1

// CannotServeRollback is the GetPruningBoundary answer meaning "I hold nothing that can serve a
// rollback to any height": in practice, a snapshot store with no completed snapshot. A store that
// answers this is not idle — it will replay forward once its first snapshot lands, so pruning the
// others now can delete the range it will replay from. The collector therefore stops:
//
//	RollbackWindow > 0  → the whole cycle is abandoned; nothing is pruned
//	RollbackWindow == 0 → this store is ignored and the others are pruned
//
// 0 is a safe value for this because block heights start at 1 and a store is only asked when its
// own cut line is above 0, so a real boundary is always positive. It also puts the conservative
// answer on Go's zero value: a store that returns nothing meaningful stops pruning rather than
// silently dropping out of the decision.
//
// Two limits are worth knowing. RollbackWindow == 0 puts the cut line at the head itself, so
// contiguous stores are pruned to their head and a store that has not snapshotted yet loses that
// replay range outright — do not run it on a node whose snapshot stores are still bootstrapping.
// And this signal only reaches the collector if the store is asked, which happens only when its
// own cut line is above 0: a snapshot store with InfiniteRetentionWindow, or with a retention
// window deep enough to zero its cut line, is never asked and so cannot stop a cycle. The SC/SS
// retention contract below pins retention at 0 to keep them out of that case.
const CannotServeRollback uint64 = 0

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
type PrunableStore interface {
	// Name identifies the store for logs and errors. Duplicate names are allowed; the
	// collector keys answers by index, not by name.
	Name() string

	// PruneBelow may drop data for all blocks below blockNumber. The store may perform the
	// deletion asynchronously. Only called when ExternalPruning reports true.
	PruneBelow(blockNumber uint64) error

	// ExternalPruning reports whether this store's retention is the collector's to enforce.
	//
	//	true  → the collector prunes it; any pruner inside the store must stand down
	//	false → the store prunes itself; the collector never calls PruneBelow on it
	//
	// This exists so that "the collector manages this store" and "the store's own pruner is
	// off" are one fact rather than two settings that can disagree. A store with an internal
	// pruner answers from the same field that pruner consults, which is what makes the two
	// mutually exclusive by construction instead of by wiring discipline. A store with no
	// pruner of its own returns true unconditionally.
	//
	// false does NOT withdraw the store from the decision. It is still asked for
	// GetLatestBlock and GetPruningBoundary, and still holds the shared minimum down, because
	// what a self-pruning store retains can be exactly what another store must replay from —
	// SC's snapshots are useless if the WAL beneath them has been pruned away. Dropping it
	// from the vote would prune that range out from under it. Opting out of being pruned and
	// opting out of protecting others are different things, and only the first is on offer.
	//
	// Read once per cycle, so a store may change its answer between cycles but not within one.
	ExternalPruning() bool

	// GetRetentionWindow is how many extra blocks beyond the shared RollbackWindow this store
	// needs to keep servable, where head is the min non-zero GetLatestBlock. Three answers:
	//
	//	> 0  → extra history on top; cutLine = head - RollbackWindow - retention
	//	0    → no extra history;     cutLine = head - RollbackWindow
	//	-1   → never prune this store at all (see InfiniteRetentionWindow)
	//
	// RollbackWindow is shared so every store stays consistent under rollback. Retention is
	// optional history on top, so a store can still serve queries after a rollback has consumed
	// the whole window.
	//
	// This is an input to a shared minimum, not an independently applied per-store policy:
	// because every participating store is pruned to the lowest boundary, one store's deep
	// retention extends retention for all of them (see StorageGarbageCollector).
	//
	// By store kind:
	//   - Contiguous (blockDB, receiptDB, state WAL): -1 / 0 / positive as configured.
	//   - SC: 0. It only needs the shared rollback window for its snapshots.
	//   - SS: 0, including on an archive node. What the collector manages for SS is snapshot
	//     pruning, not state pruning, and those snapshots only need to cover RollbackWindow. SS
	//     keeps its own version history via KeepRecent, so making a node archival belongs there,
	//     not here. Returning -1 would freeze SS snapshots forever without retaining any extra
	//     state — the opposite trade from the one intended — and would also drop SS out of the
	//     shared minimum, per the warning below.
	//
	// Do not give a snapshot store InfiniteRetentionWindow expecting deeper history — it does the
	// opposite. A store with no cut line is never asked for a boundary, so it drops out of the
	// minimum and the contiguous store holding its replay range is pruned to its own cut line.
	// With retention 0 the same store answers its oldest needed snapshot and pulls that range down
	// with it. Concretely, with head 100_000, RollbackWindow 1_000, and a lone snapshot at 20_000:
	// retention 0 holds the WAL at 20_000, while InfiniteRetentionWindow lets it go to 99_000 and
	// leaves the snapshot unable to replay forward. Only the snapshot itself survives, so this is
	// safe solely for a store that never needs another store's history — which is what the
	// StorageGarbageCollector precondition requires. TestPruneInfiniteRetentionSnapshotStore pins
	// both directions.
	GetRetentionWindow() int64

	// GetPruningBoundary returns the oldest block this store must keep in order to serve cutLine:
	//
	//	> 0                 → that boundary, which must never exceed cutLine
	//	CannotServeRollback → no snapshots available; abandons the cycle
	//
	// There is no third answer: a store either has a boundary to report or cannot serve a
	// rollback at all.
	//
	// Contiguous stores can restore to any height they hold, so they return cutLine. Snapshot
	// stores can restore only at a snapshot, so they return the newest completed snapshot at or
	// below cutLine; when every snapshot sits above cutLine none of them can be dropped, and they
	// return cutLine rather than their oldest snapshot. With no completed snapshot at all they
	// return CannotServeRollback.
	//
	// A store holding nothing at or below cutLine — empty, already pruned to a higher floor, or
	// restored by state sync above it — still returns cutLine. The PruneBelow that follows is a
	// no-op, which is the point: it has nothing to delete and nothing to protect, since a
	// contiguous store fills from its own ingest path rather than by replaying another store.
	// CannotServeRollback would be wrong there, stalling the fleet until the head advanced a full
	// RollbackWindow past that floor.
	//
	// Never answering above cutLine is what makes pruneHeight <= head - RollbackWindow hold by
	// construction, since the collector takes a minimum across stores and does not clamp. A higher
	// answer would raise that minimum; with no correctly answering retention-0 store to cap it, it
	// can exceed the head and drop the store outright.
	//
	// Nothing in the collector enforces this — honoring it is the implementor's job. The collector
	// cannot repair a bad answer, because cutLine is all it knows: substituting it would still prune
	// past the snapshot a snapshot store needed, and refusing to prune would let one faulty store
	// stall every other store's pruning. So this bound is load-bearing and unchecked.
	//
	// A snapshot write in flight need not be reserved, as snapshot creation is assumed to finish
	// quickly. Never having produced one is the separate CannotServeRollback case.
	GetPruningBoundary(cutLine uint64) uint64

	// GetLatestBlock returns the highest block this store has ingested.
	//
	// 0 means "nothing ingested yet" and is excluded when computing the head, so a store still
	// filling cannot drag the head — and every cut line with it — down to 0. That exclusion says
	// nothing about whether the store may be pruned around: one that cannot serve a rollback
	// still stops the cycle, via GetPruningBoundary.
	//
	// 0 does not mean "disabled". A store disabled for this node is not instantiated and never
	// reaches the collector.
	GetLatestBlock() (uint64, error)
}
