package walrus

// Catalog records every file this instance has on disk and is the authority on when one may be deleted.
//
// It holds two kinds of thing, because there are only two lifecycles: a pod, whose data file, index, and
// bloom filter are written together by one build and die together, and a snapshot. Every handle stays
// resident, so the catalog answers from memory.
//
// One snapshot is distinguished as the floor, and the invariant that gives every query an answer is that the
// floor exists and pods cover every block from just above it through the newest block written. A fresh
// instance satisfies it with the block 0 pseudo-snapshot.
//
// Deletion is gated by two independent questions, asked per object: does policy want it gone, and is anyone
// reading it. Policy is the query floor; readers are counted references. Keeping them separate is what lets
// collection make progress under load — raising the floor stops new queries from referencing anything below
// it, the queries already running drain, and collection proceeds.
//
// A Catalog is safe for concurrent use.
type Catalog interface {

	// AddPod registers a newly written pod, making it queryable.
	AddPod(pod *Pod)

	// AddSnapshot registers a newly retained snapshot.
	//
	// Snapshots land at arbitrary block heights and bear no relationship to pod boundaries, so adding one
	// does not by itself change the floor. Collect chooses the floor.
	AddSnapshot(snapshot Snapshot)

	// Query admits a read at blockNumber, resolving the snapshot it would terminate at and the span of pods
	// it would walk, and taking a reference to each so none can be deleted while the read runs.
	//
	// admitted is false when blockNumber is below the query floor, which is the caller's ReadTooOld. Every
	// admitted query must be released.
	Query(blockNumber uint64) (query Query, admitted bool)

	// SetQueryFloor raises the oldest block that may be queried. It never lowers it.
	//
	// This is a request, not an act: it stops new queries from reaching below blockNumber, but deletes
	// nothing. Collect does the deleting, and may land the floor below what was asked for.
	SetQueryFloor(blockNumber uint64)

	// Collect advances the floor toward the query floor and deletes everything below it, reporting how many
	// files went and how many bytes that reclaimed.
	//
	// The floor moves from one snapshot to a newer one, never to an arbitrary block: the catalog picks the
	// newest snapshot backed by real data at or below the requested floor, and deletes every pod whose last
	// block is at or below it along with every older snapshot. If no such snapshot exists, nothing moves.
	//
	// Expressing retention as moving the floor, rather than as a pod sweep and a snapshot sweep that have to
	// agree, is what prevents a gap opening between the floor and the oldest pod. A query walking into such a
	// gap terminates on a snapshot below the missing data and reports a key absent that plainly had a value —
	// a wrong answer indistinguishable from a right one, rather than a failure.
	//
	// An object policy has selected but a query still references is left for a later call.
	Collect() (files int, bytes int64, err error)

	// Bounds reports the range of blocks the catalog can answer for.
	Bounds() (
		// If true, at least one pod is retained and first/last are valid. If false, nothing is queryable and
		// first/last are undefined.
		ok bool,
		// The lowest queryable block number, inclusive. Only valid if ok is true.
		first uint64,
		// The highest queryable block number, inclusive. Only valid if ok is true.
		last uint64,
	)
}

// Query is an admitted read at one block, and the only route to the files that read may touch.
//
// That is what makes holding deletion back an invariant rather than a convention: there is no way to reach a
// pod or a snapshot without first having been admitted, so no caller can forget to.
//
// The whole span is resolved when the query is admitted, not lazily, so a walk asks the catalog nothing
// further and cannot reach outside what it took references to.
type Query interface {

	// Block returns the block being read.
	Block() uint64

	// Floor returns the snapshot the walk terminates at: the newest one at or below the queried block. There
	// is always one, though it may be the pseudo-snapshot that reports every key absent.
	Floor() Snapshot

	// Pods returns the pods holding blocks above the floor and at or below the queried block, newest first —
	// exactly the pods a backwards walk visits, in the order it visits them.
	Pods() []*Pod

	// Release ends the read and drops the references it holds.
	Release()
}
