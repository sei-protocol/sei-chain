package mempool

import (
	"context"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// DuplicateTxCache implements TxCacheWithTTL using go-cache
type DuplicateTxCache struct {
	maxSize   int
	cache     *cache.Cache
	maxKeyLen int
}

// NewDuplicateTxCache creates a new cache with TTL for transaction keys at a
// given max size. Keys are derived from the transaction key and trimmed to at
// most maxKeyLen bytes for predictable and efficient storage. If maxKeyLen is
// zero or negative, keys are not trimmed. When the cache exceeds cacheSize, the
// least recently used entry is evicted.
//
// Note that maxKeyLen should be set with care. While a smaller value saves
// memory, it increases the risk of key collisions, which can lead to false
// positives in cache lookups. A larger value reduces collision risk but uses
// more memory. A common choice is to use the full length of a cryptographic hash
// (e.g., 32 bytes for SHA-256) to balance memory usage and collision risk.
func NewDuplicateTxCache(maxSize int, defaultExpiration time.Duration, maxKeyLen int) *DuplicateTxCache {
	return &DuplicateTxCache{
		maxSize: maxSize,
		// Force cleanup interval to 0 - otherwise go-cache leaks a goroutine.
		// TODO: replace with a more reasonable implementation of cache, which doesn't do such things.
		cache:     cache.New(defaultExpiration, 0),
		maxKeyLen: maxKeyLen,
	}
}

func (t *DuplicateTxCache) Run(ctx context.Context, cleanupInterval time.Duration) error {
	if cleanupInterval <= 0 {
		return nil
	}
	// Periodically delete the expired items.
	for {
		if err := utils.Sleep(ctx, cleanupInterval); err != nil {
			return err
		}
		t.cache.DeleteExpired()
	}
}

// Set adds a transaction to the cache with TTL
func (t *DuplicateTxCache) Set(txHash types.TxHash, counter int) {
	t.cache.SetDefault(t.toCacheKey(txHash), counter)
}

// Get retrieves the counter for a transaction key
func (t *DuplicateTxCache) Get(txHash types.TxHash) (counter int, found bool) {
	if value, exists := t.cache.Get(t.toCacheKey(txHash)); exists {
		if counter, ok := value.(int); ok {
			return counter, true
		}
	}
	return 0, false
}

// Increment increments the counter for a transaction key, extending TTL
func (t *DuplicateTxCache) Increment(txHash types.TxHash) {
	key := t.toCacheKey(txHash)
	err := t.cache.Increment(key, 1)
	if err != nil {
		// Only set a new key if the cache is not full
		if t.cache.ItemCount() < t.maxSize {
			t.cache.SetDefault(key, 1)
		}
	}
}

// Reset clears the cache
func (t *DuplicateTxCache) Reset() {
	t.cache.Flush()
}

// Stop stops the cache and cleans up background goroutines
func (t *DuplicateTxCache) Stop() {
	// go-cache doesn't have a Stop method, but we can flush it
	// The janitor goroutine will be cleaned up by the garbage collector
	// when the cache object is no longer referenced
	t.cache.Flush()
}

func (t *DuplicateTxCache) GetForMetrics() (int, int, int, int) {
	var (
		maxCount          = 0
		totalCount        = 0
		duplicateCount    = 0
		nonDuplicateCount = 0
	)
	for _, v := range t.cache.Items() {
		if counter, ok := v.Object.(int); ok {
			if counter > 1 {
				totalCount += counter - 1
				duplicateCount++
			} else {
				nonDuplicateCount++
			}
			if counter > maxCount {
				maxCount = counter
			}
		}
	}
	return maxCount, totalCount, duplicateCount, nonDuplicateCount
}

// txHashToString converts a TxHash (byte array) to a stable string key.
func (t *DuplicateTxCache) toCacheKey(key types.TxHash) duplicateCacheKey {
	return duplicateCacheKey(trimToSize(key, t.maxKeyLen))
}

func trimToSize(key types.TxHash, maxKeyLen int) []byte {
	if maxKeyLen <= 0 {
		return key[:]
	}
	return key[:min(maxKeyLen, len(key))]
}
