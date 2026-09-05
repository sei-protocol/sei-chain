package walrus

// PodAccumulator gathers blocks until they fill a pod.
//
// It is the only place that knows what a block costs on disk. The engine above it never learns the pod
// framing, and the builder below it never decides where a pod ends.
//
// A PodAccumulator is not safe for concurrent use.
type PodAccumulator interface {

	// Add stores a block.
	//
	// When the block does not fit in the pod being accumulated, that pod is returned complete and the block
	// becomes the first of the next one. pod is nil unless this call completed one.
	//
	// Blocks must arrive in contiguous ascending order. A block too large to fit in an empty pod is rejected:
	// its entries could not all be addressed by a uint32 offset.
	Add(block Block) (pod []Block, err error)

	// Drain returns the blocks gathered so far and resets, or nil if there are none.
	//
	// This is how a pod short of the size limit gets written at shutdown. Without it those blocks would be
	// lost, since only a written pod is queryable.
	Drain() []Block
}
