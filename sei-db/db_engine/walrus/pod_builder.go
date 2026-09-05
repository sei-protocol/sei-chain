package walrus

// PodBuilder writes pods.
//
// A pod is built in one shot from blocks already in memory: the builder writes the pod's data file, its
// index, and its bloom filter, then returns them open for querying. Holding a whole pod in memory is what
// makes that possible, and it is affordable at the sizes involved.
//
// Building in one shot removes the machinery an incremental writer would need. There is no append path and
// no rotation decision here, because the accumulator already chose where the pod ends. There is no pass back
// over a sealed file, because the data never left memory. And the bloom filter's key count is known from the
// sorted keys before any file is opened, so nothing has to be written in a particular order to discover it.
//
// A PodBuilder is safe for concurrent use: builds share no state, and two pods never write the same files.
type PodBuilder interface {

	// Build writes the pod holding blocks and returns it open for querying.
	//
	// blocks must be non-empty and in contiguous ascending order; the pod takes its identity from their
	// range. The returned Pod reads from disk, so nothing reachable from blocks stays pinned in memory once
	// this returns and the caller is free to release them.
	//
	// The three files are written under temporary names and renamed into place together, so an interrupted
	// build leaves no pod that a later open could mistake for a complete one.
	Build(blocks []Block) (*Pod, error)
}
