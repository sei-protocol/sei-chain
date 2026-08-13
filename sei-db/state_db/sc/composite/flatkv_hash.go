package composite

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

// flatKVHashCache holds flatkv block hashes read off its channel, so the Cosmos hash path can ask for a
// height's hash more than once without waiting more than once.
//
// Cosmos asks three times per block — for the working hash during FinalizeBlock, again inside Commit, and for
// the last commit info — so only the first ask per height can miss. It is also this cache's reads that keep
// flatkv's hash channel drained: a channel nobody reads eventually blocks the store.
//
// This exists for Cosmos and dies with it. Autobahn tolerates an asynchronous hash and will consume the
// channel directly, with no need to block on a height it has just committed.
type flatKVHashCache struct {
	// hashes is the block hash for each height read so far, keyed by height.
	hashes map[int64][]byte

	// highest is the greatest height read so far, so a hash already passed can be recognised as gone rather
	// than waited for.
	highest int64
}

// newFlatKVHashCache returns an empty cache.
func newFlatKVHashCache() *flatKVHashCache {
	return &flatKVHashCache{hashes: make(map[int64][]byte)}
}

// hashAtVersion returns the flatkv hash that describes the state at version.
//
// It is called on the commit path, which is single-threaded, and holds no lock of its own.
func (c *flatKVHashCache) hashAtVersion(store flatkv.Store, version int64) ([]byte, error) {
	// A block that has not been committed has no hash, so asking for one is asking for the commit. The Commit
	// that Cosmos issues afterwards finds the block already committed and does nothing.
	if err := store.CommitPendingBlock(); err != nil {
		return nil, fmt.Errorf("commit pending block before reading flatkv hash for version %d: %w",
			version, err)
	}

	// flatkv only has the heights it committed, and a block with no flatkv writes is not a block here at all —
	// there is nothing to commit and so nothing to hash. The hash of the newest block flatkv does have
	// describes the same state, which is what the height being asked about needs.
	height := version
	if committed := store.Version(); committed < height {
		height = committed
	}
	return c.awaitHeight(store, height)
}

// awaitHeight returns flatkv's hash for a height it has committed, reading the channel until that height
// arrives if it has not been seen yet.
func (c *flatKVHashCache) awaitHeight(store flatkv.Store, height int64) ([]byte, error) {
	if hash, ok := c.hashes[height]; ok {
		return hash, nil
	}

	// A store publishes the hash of the height it loaded at before it hashes anything, so a historical read —
	// open at version N, ask about N — is answered here without a block ever being hashed. Waiting on the
	// channel for it would wait forever: the hasher publishes N+1 onward.
	published := store.PublishedHash()
	if published.BlockHeight == height {
		return published.Hash, nil
	}
	if height < published.BlockHeight || height <= c.highest {
		// The stream is already past it, so waiting would never end.
		return nil, fmt.Errorf("flatkv hash for height %d is no longer available (published %d, read to %d)",
			height, published.BlockHeight, c.highest)
	}

	for hash := range store.HashChan() {
		c.hashes[hash.BlockHeight] = hash.Hash
		if hash.BlockHeight > c.highest {
			c.highest = hash.BlockHeight
		}
		if hash.BlockHeight >= height {
			break
		}
	}

	hash, ok := c.hashes[height]
	if !ok {
		// The channel closed before the height arrived, which means the store is failing or shutting down.
		return nil, fmt.Errorf("flatkv stopped producing hashes before height %d", height)
	}
	c.forget(height)
	return hash, nil
}

// forget drops hashes for heights below height, which nothing will ask for again.
func (c *flatKVHashCache) forget(height int64) {
	for h := range c.hashes {
		if h < height {
			delete(c.hashes, h)
		}
	}
}
