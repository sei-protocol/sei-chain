package proxy

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// checkedSendersCap bounds the senders remembered per generation.
// Entries are dropped as their transactions execute; the cap only guards against
// remembered transactions that never reach a block.
const checkedSendersCap = 1 << 22

// senderCache remembers the sender recovered for each transaction hash. It keeps
// two generations: inserts go to fresh, and once fresh reaches the cap it
// becomes stale and the previous stale generation is forgotten, so the most
// recent entries always survive a rollover.
type senderCache struct {
	fresh, stale map[common.Hash]common.Address
	// capacity is how many entries a generation holds before it rolls over.
	capacity int
}

func newSenderCache(capacity int) senderCache {
	return senderCache{fresh: map[common.Hash]common.Address{}, capacity: capacity}
}

func (c *senderCache) put(hash common.Hash, sender common.Address) {
	if len(c.fresh) >= c.capacity {
		// No size hint: pre-sizing or clearing a generation costs O(capacity) under the lock.
		c.stale, c.fresh = c.fresh, map[common.Hash]common.Address{}
	}
	c.fresh[hash] = sender
}

// peek returns the sender remembered for hash, if any, and keeps it.
func (c *senderCache) peek(hash common.Hash) utils.Option[common.Address] {
	for _, gen := range [...]map[common.Hash]common.Address{c.fresh, c.stale} {
		if sender, ok := gen[hash]; ok {
			return utils.Some(sender)
		}
	}
	return utils.None[common.Address]()
}

// take returns the sender remembered for hash, if any, and forgets it.
func (c *senderCache) take(hash common.Hash) utils.Option[common.Address] {
	for _, gen := range [...]map[common.Hash]common.Address{c.fresh, c.stale} {
		if sender, ok := gen[hash]; ok {
			delete(gen, hash)
			return utils.Some(sender)
		}
	}
	return utils.None[common.Address]()
}
