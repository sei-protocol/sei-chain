package types

import (
	"sync"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	MESSAGE_COUNT	= "message_count"
	TX_COUNT		= "transaction_count"
	ORDER_COUNT		= "order_count"
)

type ContextMemCache struct {
	deferredBankOpsLock *sync.Mutex
	deferredSends       *DeferredBankOperationMapping
	deferredWithdrawals *DeferredBankOperationMapping

	metricsLock 		*sync.RWMutex
	metricsCounterMapping *map[string]uint32
}

func NewContextMemCache() *ContextMemCache {
	return &ContextMemCache{
		deferredBankOpsLock: &sync.Mutex{},
		deferredSends:       NewDeferredBankOperationMap(),
		deferredWithdrawals: NewDeferredBankOperationMap(),
		metricsLock: &sync.RWMutex{},
		metricsCounterMapping: &map[string]uint32{},
	}
}

func (c *ContextMemCache) GetDeferredSends() *DeferredBankOperationMapping {
	return c.deferredSends
}

func (c *ContextMemCache) GetDeferredWithdrawals() *DeferredBankOperationMapping {
	return c.deferredWithdrawals
}

func (c *ContextMemCache) IncrMetricCounter(count uint32, metric_name string)  {
	c.metricsLock.Lock()
	defer c.metricsLock.Unlock()

	newCounter := (*c.metricsCounterMapping)[metric_name] + count
	(*c.metricsCounterMapping)[metric_name] = newCounter
}

func (c *ContextMemCache) GetMetricCounters() *map[string]uint32{
	c.metricsLock.RLock()
	defer c.metricsLock.RUnlock()
	return c.metricsCounterMapping
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

	c.metricsLock.Lock()
	defer c.metricsLock.Unlock()
	c.metricsCounterMapping = &map[string]uint32{}
}
