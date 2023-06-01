package types

import (
	"sync"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type ContextMemCache struct {
	deferredBankOpsLock *sync.Mutex
	deferredSends       *DeferredBankOperationMapping
	deferredWithdrawals *DeferredBankOperationMapping
}

func NewContextMemCache() *ContextMemCache {
	return &ContextMemCache{
		deferredBankOpsLock: &sync.Mutex{},
		deferredSends:       NewDeferredBankOperationMap(),
		deferredWithdrawals: NewDeferredBankOperationMap(),
	}
}

func (c *ContextMemCache) GetDeferredSends() *DeferredBankOperationMapping {
	return c.deferredSends
}

func (c *ContextMemCache) GetDeferredWithdrawals() *DeferredBankOperationMapping {
	return c.deferredWithdrawals
}

func (c *ContextMemCache) UpsertDeferredSends(moduleAccount string, amount Coins) error {
	if !amount.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String())
	}

	// Separate locks needed for all mappings - atomic transaction needed
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	// If there's already a pending withdrawal then subtract it from that amount first
	// or else add it to the deferredSends mapping
	ok := c.deferredWithdrawals.SafeSub(moduleAccount, amount)
	if !ok {
		c.deferredSends.UpsertMapping(moduleAccount, amount)
	}
	return nil
}

func (c *ContextMemCache) SafeSubDeferredSends(moduleAccount string, amount Coins) bool {
	if !amount.IsValid() {
		panic(sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String()))
	}

	return c.deferredSends.SafeSub(moduleAccount, amount)
}

func (c *ContextMemCache) RangeOnDeferredSendsAndDelete(apply func(recipient string, amount Coins)) {
	c.deferredSends.RangeOnMapping(apply)
}

func (c *ContextMemCache) UpsertDeferredWithdrawals(moduleAccount string, amount Coins) error {
	if !amount.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String())
	}

	// Separate locks needed for all mappings - atmoic transaction needed
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	// If there's already a pending deposit then subtract it from that amount first
	// or else add it to the deferredWithdrawals mapping
	ok := c.deferredSends.SafeSub(moduleAccount, amount)
	if !ok {
		c.deferredWithdrawals.UpsertMapping(moduleAccount, amount)
	}
	return nil
}

// This inserts or updates an entry for a module account for a deferred withdrawal.
// This should be performed AFTER checking for a sufficient balance in the underlying bank balances for that module account.
// Additionally, this does not attempt to do any safe subtraction from pending sends, so if that behavior is preferred, it will need to be checked separately.
func (c *ContextMemCache) UpsertDeferredWithdrawalsNoSafeSub(moduleAccount string, amount Coins) error {
	if !amount.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amount.String())
	}

	// Separate locks needed for all mappings - atmoic transaction needed
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()

	c.deferredWithdrawals.UpsertMapping(moduleAccount, amount)
	return nil
}

func (c *ContextMemCache) RangeOnDeferredWithdrawalsAndDelete(apply func(recipient string, amount Coins)) {
	c.deferredWithdrawals.RangeOnMapping(apply)
}

func (c *ContextMemCache) ApplyOnAllDeferredOperationsAndDelete(apply func(recipient string, amount Coins)) {
	c.RangeOnDeferredSendsAndDelete(apply)
	c.RangeOnDeferredWithdrawalsAndDelete(apply)
}

func (c *ContextMemCache) Clear() {
	c.deferredBankOpsLock.Lock()
	defer c.deferredBankOpsLock.Unlock()
	c.deferredSends = NewDeferredBankOperationMap()
	c.deferredWithdrawals = NewDeferredBankOperationMap()
}
