package lthash

import "github.com/sei-protocol/sei-chain/sei-db/common/threading"

// Computes lattice hashes for flatKV.
type HashEngine struct {
}

// TODO create a config

func NewHashEngine(pool threading.Pool, dbDirs []string, moduleOf ModuleFunc) (*HashEngine, error) {
	return nil, nil // TODO
}

// Schedule a block to be hashed.
func (he *HashEngine) ScheduleHash(current *storeView, previous *storeView) error { // TODO Claude: we need to move storeView and the atomic store view to a new package called flatkv/sview
	// TODO

	// Three phases of hashing, which should be fully pipelined.
	// 1. collect key-value pairs we need to hash from the storeView objects, we can use a single worker thread for this
	// 2. fan out to thread pool to hash key-value pairs, ok if multiple blocks are in this phase at once
	// 3. single thread that stitches hashes together, on block at a time in block order (since block N depends on block N-1)

	// Phase 1 and 3 should have a dedicated goroutine, phase 2 should use the pool in the constructor.
	// Communication to and from each of these phases should happen via channels. 
	// - channel from ScheduleHash to phase 1 worker
	// - channel from phase 1 worker to each of the pool workers (managed internally by the pool)
	// - channel from each phase 2 worker to the phase 3 worker
	// - channel from phase 3 worker to AwaitHash()

	return nil
}

// Returns a channel that returns block hashes, as they are computed.
func (he *HashCalculator) AwaitHash() <-chan *BlockHash {
	return nil // TODO
}
