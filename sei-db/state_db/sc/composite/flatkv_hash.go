package composite

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

// flatKVHashCache answers Cosmos's synchronous hash questions from flatKV's asynchronous hash stream.
//
// Cosmos asks three times per block — for the working hash during FinalizeBlock, again inside Commit,
// and once more for the last commit info — so only the first ask per height can miss. It is also this
// cache's reads that keep flatKV's hash channel drained: a channel nobody reads eventually blocks
// commit.
//
// This exists for Cosmos and dies with it. A caller that tolerates an asynchronous hash consumes the
// channel directly.
//
// Not safe for concurrent use. Cosmos's hash path is single-threaded, and the composite store's lock
// serializes the callers that reach it.
type flatKVHashCache struct {
	// hashes holds the heights read off the stream but not yet asked for.
	hashes map[int64][]byte

	// highest is the greatest height read so far, so that a height already passed is reported as gone
	// rather than waited for. The stream only moves forwards.
	highest int64
}

func newFlatKVHashCache() *flatKVHashCache {
	return &flatKVHashCache{hashes: make(map[int64][]byte)}
}

// hashAtVersion returns flatKV's lattice hash for the given height, committing the block first if it is
// still being applied.
func (c *flatKVHashCache) hashAtVersion(store giga.LiveStateStore, version int64) ([]byte, error) {
	// A block that has not been committed has no hash, so asking for one is asking for the commit.
	if err := store.CommitPendingBlock(); err != nil {
		return nil, fmt.Errorf("seal flatkv block %d before hashing: %w", version, err)
	}

	// A block none of whose writes reached flatKV leaves it a height behind. Its hash has not moved —
	// an empty block does not shift the lattice — so the height it did reach is the right answer.
	if committed := store.Version(); committed < version {
		version = committed
	}
	return c.awaitHeight(store, version)
}

// awaitHeight reports the hash for height, reading the stream until it arrives.
func (c *flatKVHashCache) awaitHeight(store giga.LiveStateStore, height int64) ([]byte, error) {
	if hash, ok := c.hashes[height]; ok {
		c.forget(height)
		return hash, nil
	}

	// A store publishes the hash of the height it loaded at before it hashes anything, so a historical
	// read — open at version N, ask about N — is answered here without a block ever being hashed.
	// Waiting on the stream for it would wait forever.
	published := store.PublishedHash()
	if published.BlockNumber == height {
		checksum := published.Global.Checksum()
		return checksum[:], nil
	}
	if height < published.BlockNumber || height <= c.highest {
		return nil, fmt.Errorf("flatkv hash for block %d is no longer available: the stream has reached %d",
			height, max(published.BlockNumber, c.highest))
	}

	for hash := range store.HashChan() {
		checksum := hash.Global.Checksum()
		c.hashes[hash.BlockNumber] = checksum[:]
		if hash.BlockNumber > c.highest {
			c.highest = hash.BlockNumber
		}
		if hash.BlockNumber >= height {
			break
		}
	}

	result, ok := c.hashes[height]
	if !ok {
		return nil, fmt.Errorf("flatkv stopped producing hashes before block %d", height)
	}
	c.forget(height)
	return result, nil
}

// forget drops every height at or below the one just answered. The stream is one-directional, so
// nothing below can be asked for again.
func (c *flatKVHashCache) forget(height int64) {
	for cached := range c.hashes {
		if cached <= height {
			delete(c.hashes, cached)
		}
	}
}
