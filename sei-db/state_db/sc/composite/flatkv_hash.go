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

// hashAtVersion returns flatkv's block hash for version, committing the pending block and then reading the
// channel until that height arrives if it has not been seen yet.
//
// It is called on the commit path, which is single-threaded, and holds no lock of its own.
func (c *flatKVHashCache) hashAtVersion(store flatkv.Store, version int64) ([]byte, error) {
	if hash, ok := c.hashes[version]; ok {
		return hash, nil
	}

	// A store publishes the hash of the height it loaded at before it hashes anything, so a historical read —
	// open at version N, ask for N — is answered here without a block ever being hashed. Waiting on the stream
	// for it would wait forever: the hasher publishes N+1 onward.
	published := store.PublishedHash()
	if published.BlockHeight == version {
		return published.Hash, nil
	}
	if version < published.BlockHeight || version <= c.highest {
		// The stream is already past it, so waiting would never end.
		return nil, fmt.Errorf("flatkv hash for version %d is no longer available (published %d, read to %d)",
			version, published.BlockHeight, c.highest)
	}

	// A block that has not been committed has no hash, so asking for one is asking for the commit. The Commit
	// that Cosmos issues afterwards finds the block already committed and does nothing.
	if err := store.CommitPendingBlock(); err != nil {
		return nil, fmt.Errorf("commit pending block before reading flatkv hash for version %d: %w",
			version, err)
	}

	for hash := range store.HashChan() {
		c.hashes[hash.BlockHeight] = hash.Hash
		if hash.BlockHeight > c.highest {
			c.highest = hash.BlockHeight
		}
		if hash.BlockHeight >= version {
			break
		}
	}

	hash, ok := c.hashes[version]
	if !ok {
		// The channel closed before the height arrived, which means the store is failing or shutting down.
		return nil, fmt.Errorf("flatkv stopped producing hashes before version %d", version)
	}
	c.forget(version)
	return hash, nil
}

// forget drops hashes for heights below version, which nothing will ask for again.
func (c *flatKVHashCache) forget(version int64) {
	for height := range c.hashes {
		if height < version {
			delete(c.hashes, height)
		}
	}
}
