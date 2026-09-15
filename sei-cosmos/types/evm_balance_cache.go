package types

import "math/big"

type multiStoreIdentity byte

func newMultiStoreIdentity() *multiStoreIdentity {
	return new(multiStoreIdentity)
}

// EVMBalanceCache stores spendable EVM balances for one execution request.
type EVMBalanceCache struct {
	entries map[string]cachedEVMBalance
}

type cachedEVMBalance struct {
	multiStoreIdentity *multiStoreIdentity
	balance            big.Int
}

// NewEVMBalanceCache returns an empty EVM balance cache.
func NewEVMBalanceCache() *EVMBalanceCache {
	return &EVMBalanceCache{entries: map[string]cachedEVMBalance{}}
}

// EVMBalanceCache returns the balance cache attached to the context, if any.
func (c Context) EVMBalanceCache() *EVMBalanceCache {
	return c.evmBalanceCache
}

// WithEVMBalanceCache returns a context that uses cache for EVM balance reads.
func (c Context) WithEVMBalanceCache(cache *EVMBalanceCache) Context {
	c.evmBalanceCache = cache
	return c
}

// GetCachedEVMBalance returns the cached balance for addr in the active multistore layer.
func (c Context) GetCachedEVMBalance(addr AccAddress) (*big.Int, bool) {
	if c.evmBalanceCache == nil {
		return nil, false
	}
	entry, ok := c.evmBalanceCache.entries[string(addr)]
	if !ok || entry.multiStoreIdentity != c.multiStoreIdentity {
		return nil, false
	}
	return new(big.Int).Set(&entry.balance), true
}

// SetCachedEVMBalance stores balance for addr in the active multistore layer.
func (c Context) SetCachedEVMBalance(addr AccAddress, balance *big.Int) {
	if c.evmBalanceCache == nil {
		return
	}
	entry := cachedEVMBalance{multiStoreIdentity: c.multiStoreIdentity}
	entry.balance.Set(balance)
	c.evmBalanceCache.entries[string(addr)] = entry
}

// InvalidateCachedEVMBalance removes any cached balance for addr.
func (c Context) InvalidateCachedEVMBalance(addr AccAddress) {
	if c.evmBalanceCache == nil {
		return
	}
	delete(c.evmBalanceCache.entries, string(addr))
}
