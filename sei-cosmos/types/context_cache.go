package types

import (
	"sort"
	"sync"
)

type ContextMemCache struct {
	deferredSends 			  map[string]Coins
	deferredSendsLock		  *sync.Mutex
}

func NewContextMemCache() *ContextMemCache {
	return &ContextMemCache{
		deferredSends: make(map[string]Coins),
		deferredSendsLock: &sync.Mutex{},
	}
}

func (c *ContextMemCache) UpsertDeferredSends(recipientModule string, amount Coins) {
	// The whole operation needs to be atomic
	c.deferredSendsLock.Lock()
	defer c.deferredSendsLock.Unlock()

	newAmount := amount
	if v, ok := c.deferredSends[recipientModule]; ok {
		newAmount = v.Add(amount...)
	}
	c.deferredSends[recipientModule] = newAmount
}

func (c *ContextMemCache) RangeOnDeferredSendsAndDelete(apply func (recipient string, amount Coins)) {
	// The whole operation needs to be atomic
	c.deferredSendsLock.Lock()
	defer c.deferredSendsLock.Unlock()

	// Need to sort keys for deterministic iterating
	keys := make([]string, 0, len(c.deferredSends))
	for key := range c.deferredSends {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, recipientModule := range keys {
		apply(recipientModule, c.deferredSends[recipientModule])
	}

	for _, recipientModule := range keys {
		delete(c.deferredSends, recipientModule)
	}
}

