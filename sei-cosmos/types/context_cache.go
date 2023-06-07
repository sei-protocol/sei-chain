package types

import (
	"sync"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type ContextMemCache struct {
	deferredBankOpsLock *sync.Mutex
	deferredSends       *DeferredBankOperationMapping
}

func NewContextMemCache() *ContextMemCache {
	return &ContextMemCache{
		deferredBankOpsLock: &sync.Mutex{},
		deferredSends:       NewDeferredBankOperationMap(),
	}
}

func (c *ContextMemCache) GetDeferredSends() *DeferredBankOperationMapping {
	return c.deferredSends
}

func (c *ContextMemCache) UpsertDeferredSends(moduleAccount string, amount Coins) error {
	if !amount.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String())
	}

	// Separate locks needed for all mappings - atomic transaction needed
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	c.deferredSends.UpsertMapping(moduleAccount, amount)
	return nil
}

// This will perform a saturating sub against the deferred module balance atomically. This means that it will subtract any balances that it is able to, and then call the passed in `remainder handler` to ensure the remaining balance can be safely subtracted as well. If this remainderHandler returns an error, we will revert the saturating sub to ensure atomicity of the subtraction across the multiple balances
func (c *ContextMemCache) AtomicSpilloverSubDeferredSends(moduleAccount string, amount Coins, remainderHandler func(amount Coins) error) error {
	if !amount.IsValid() {
		panic(sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String()))
	}
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	originalBalance, ok := c.deferredSends.Get(moduleAccount)
	if !ok {
		// run the remainder handler on the full amount
		err := remainderHandler(amount)
		// no need for revert because we dont even attempt a saturating sub, bubble up error if applicable
		return err
	}
	// found a balance, try to perform the logic
	remainder := c.deferredSends.SaturatingSub(moduleAccount, amount)
	if remainder.IsZero() {
		// no remainder, return now
		return nil
	}
	err := remainderHandler(remainder)
	if err != nil {
		// revert the map subtraction and the bubble up error
		c.deferredSends.Set(moduleAccount, originalBalance)
	}
	return err
}

func (c *ContextMemCache) SafeSubDeferredSends(moduleAccount string, amount Coins) bool {
	if !amount.IsValid() {
		panic(sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String()))
	}
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	return c.deferredSends.SafeSub(moduleAccount, amount)
}

func (c *ContextMemCache) RangeOnDeferredSendsAndDelete(apply func(recipient string, amount Coins)) {
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()
	c.deferredSends.RangeAndRemove(apply)
}

func (c *ContextMemCache) Clear() {
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()
	c.deferredSends = NewDeferredBankOperationMap()
}
