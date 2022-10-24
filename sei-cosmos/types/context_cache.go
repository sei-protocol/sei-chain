package types

import (
	"sync"
)


type ContextMemCache struct {
	deferredBankOpsLock		  *sync.Mutex
	deferredSends 			  *DeferredBankOperationMapping
	deferredWithdrawals 	  *DeferredBankOperationMapping

}

func NewContextMemCache() *ContextMemCache {
	return &ContextMemCache{
		deferredBankOpsLock: &sync.Mutex{},
		deferredSends: NewDeferredBankOperationMap(),
		deferredWithdrawals: NewDeferredBankOperationMap(),
	}
}

func (c *ContextMemCache) UpsertDeferredSends(moduleAccount string, amount Coins) {
	// Separate locks needed for all mappings - atomic transaction needed
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	// If there's already a pending withdrawal then subtract it from that amount first
	// or else add it to the deferredSends mapping
	ok := c.deferredWithdrawals.SafeSub(moduleAccount, amount)
	if !ok {
		c.deferredSends.UpsertMapping(moduleAccount, amount)
	}
}

func (c *ContextMemCache) RangeOnDeferredSendsAndDelete(apply func (recipient string, amount Coins)) {
	c.deferredSends.RangeOnMapping(apply)
}

func (c *ContextMemCache) UpsertDeferredWithdrawals(moduleAccount string, amount Coins) {
	// Separate locks needed for all mappings - atomic transaction needed
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	// If there's already a pending deposit then subtract it from that amount first
	// or else add it to the deferredWithdrawals mapping
	ok := c.deferredSends.SafeSub(moduleAccount, amount)
	if !ok {
		c.deferredWithdrawals.UpsertMapping(moduleAccount, amount)
	}
}

func (c *ContextMemCache) RangeOnDeferredWithdrawalsAndDelete(apply func (recipient string, amount Coins)) {
	c.deferredWithdrawals.RangeOnMapping(apply)
}

func (c *ContextMemCache) ApplyOnAllDeferredOperationsAndDelete(apply func (recipient string, amount Coins)) {
	c.RangeOnDeferredSendsAndDelete(apply)
	c.RangeOnDeferredWithdrawalsAndDelete(apply)
}
