package lthash

// The messages the phases send one another, and the state one block carries as it moves between them.

// submittedBlock is one block whose leaf hashing has been submitted, waiting to be folded onto the
// running hash.
type submittedBlock struct {
	// blockNumber is the height this job hashes.
	blockNumber int64

	// hashes is this block's leaf hashing in flight, which the combiner drains to completion.
	hashes leafHashes

	// err is set when this block's chunks could not be produced at all, in which case hashes is zero and
	// the combiner fails the block rather than reading results.
	err error
}

// chunkResult is one chunk of one block, folded.
type chunkResult struct {
	key  ModuleKey
	info *ModuleHashInfo
}

// flushRequest asks the engine to report once it has dealt with everything queued ahead of it.
type flushRequest struct {
	// done is closed once every message queued ahead of this one has been dealt with. A channel rather
	// than a value, so the engine answering it can never block on a caller that has given up.
	doneChan chan struct{}
}

func newFlushRequest() *flushRequest {
	return &flushRequest{doneChan: make(chan struct{})}
}
