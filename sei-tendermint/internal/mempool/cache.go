package mempool

import (
	"container/list"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/patrickmn/go-cache"
	"github.com/tendermint/tendermint/types"
)

// TxCache defines an interface for raw transaction caching in a mempool.
// Currently, a TxCache does not allow direct reading or getting of transaction
// values. A TxCache is used primarily to push transactions and removing
// transactions. Pushing via Push returns a boolean telling the caller if the
// transaction already exists in the cache or not.
type TxCache interface {
	// Reset resets the cache to an empty state.
	Reset()

	// Push adds the given transaction key to the cache and returns true if it was
	// newly added. Otherwise, it returns false.
	Push(tx types.TxKey) bool

	// Remove removes the given transaction key from the cache.
	Remove(tx types.TxKey)

	// Size returns the current size of the cache
	Size() int
}

var _ TxCache = (*LRUTxCache)(nil)

// LRUTxCache maintains a thread-safe LRU cache of raw transactions. The cache
// only stores the hash of the raw transaction.
type LRUTxCache struct {
	mtx       sync.Mutex
	size      int
	cacheMap  map[cacheKey]*list.Element
	list      *list.List
	maxKeyLen int
}

type cacheKey = string

// NewLRUTxCache creates an LRU (Least Recently Used) cache that stores
// transactions by key. Keys are derived from the transaction key and trimmed to
// at most maxKeyLen bytes for predictable and efficient storage. If maxKeyLen is
// zero or negative, keys are not trimmed. When the cache exceeds cacheSize, the
// least recently used entry is evicted.
//
// Note that maxKeyLen should be set with care. While a smaller value saves
// memory, it increases the risk of key collisions, which can lead to false
// positives in cache lookups. A larger value reduces collision risk but uses
// more memory. A common choice is to use the full length of a cryptographic hash
// (e.g., 32 bytes for SHA-256) to balance memory usage and collision risk.
func NewLRUTxCache(cacheSize int, maxKeyLen int) *LRUTxCache {
	return &LRUTxCache{
		size:      cacheSize,
		cacheMap:  make(map[cacheKey]*list.Element, cacheSize),
		list:      list.New(),
		maxKeyLen: maxKeyLen,
	}
}

func (c *LRUTxCache) Reset() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.cacheMap = make(map[cacheKey]*list.Element, c.size)
	c.list.Init()
}

func (c *LRUTxCache) Push(txKey types.TxKey) bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	key := c.toCacheKey(txKey)
	moved, ok := c.cacheMap[key]
	if ok {
		c.list.MoveToBack(moved)
		return false
	}

	if c.list.Len() >= c.size {
		front := c.list.Front()
		if front != nil {
			frontKey := front.Value.(cacheKey)
			delete(c.cacheMap, frontKey)
			c.list.Remove(front)
		}
	}

	e := c.list.PushBack(key)
	c.cacheMap[key] = e

	return true
}

func (c *LRUTxCache) Remove(txKey types.TxKey) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	key := c.toCacheKey(txKey)
	e := c.cacheMap[key]
	delete(c.cacheMap, key)

	if e != nil {
		c.list.Remove(e)
	}
}

func (c *LRUTxCache) Size() int {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.list.Len()
}

func (c *LRUTxCache) toCacheKey(key types.TxKey) cacheKey {
	return cacheKey(trimToSize(key, c.maxKeyLen))
}

// NopTxCache defines a no-op raw transaction cache.
type NopTxCache struct{}

var _ TxCache = (*NopTxCache)(nil)

func (NopTxCache) Reset()                {}
func (NopTxCache) Push(types.TxKey) bool { return true }
func (NopTxCache) Remove(types.TxKey)    {}
func (NopTxCache) Size() int             { return 0 }

// NopTxCacheWithTTL defines a no-op TTL transaction cache.
type NopTxCacheWithTTL struct{}

var _ TxCacheWithTTL = (*NopTxCacheWithTTL)(nil)

func (NopTxCacheWithTTL) Set(types.TxKey, int)                {}
func (NopTxCacheWithTTL) Get(types.TxKey) (int, bool)         { return 0, false }
func (NopTxCacheWithTTL) Increment(types.TxKey)               {}
func (NopTxCacheWithTTL) Reset()                              {}
func (NopTxCacheWithTTL) GetForMetrics() (int, int, int, int) { return 0, 0, 0, 0 }
func (NopTxCacheWithTTL) Stop()                               {}

// TxCacheWithTTL defines an interface for TTL-based transaction caching
type TxCacheWithTTL interface {
	// Set adds a transaction to the cache with TTL
	Set(txKey types.TxKey, counter int)

	// Get retrieves the counter for a transaction key
	Get(txKey types.TxKey) (counter int, found bool)

	// Increment increments the counter for a transaction key, extending TTL
	Increment(txKey types.TxKey)

	// GetForMetrics returns the max count, total count, duplicate count, and non duplicate count
	GetForMetrics() (int, int, int, int)

	// Reset clears the cache
	Reset()

	// Stop stops the cache and cleans up background goroutines
	Stop()
}

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
func NewDuplicateTxCache(maxSize int, defaultExpiration, cleanupInterval time.Duration, maxKeyLen int) *DuplicateTxCache {
	// If defaultExpiration is 0 (no expiration), don't create a cleanup interval
	// to avoid starting background janitor goroutines that can cause leaks
	if defaultExpiration == 0 {
		cleanupInterval = 0
		log.Debug().Msg("TTL cache expiration disabled")
	}

	return &DuplicateTxCache{
		maxSize:   maxSize,
		cache:     cache.New(defaultExpiration, cleanupInterval),
		maxKeyLen: maxKeyLen,
	}
}

// Set adds a transaction to the cache with TTL
func (t *DuplicateTxCache) Set(txKey types.TxKey, counter int) {
	t.cache.SetDefault(t.toCacheKey(txKey), counter)
}

// Get retrieves the counter for a transaction key
func (t *DuplicateTxCache) Get(txKey types.TxKey) (counter int, found bool) {
	if value, exists := t.cache.Get(t.toCacheKey(txKey)); exists {
		if counter, ok := value.(int); ok {
			return counter, true
		}
	}
	return 0, false
}

// Increment increments the counter for a transaction key, extending TTL
func (t *DuplicateTxCache) Increment(txKey types.TxKey) {
	key := t.toCacheKey(txKey)
	err := t.cache.Increment(key, 1)
	if err != nil {
		t.cache.SetDefault(key, 1)
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

// txKeyToString converts a TxKey (byte array) to a stable string key.
func (t *DuplicateTxCache) toCacheKey(key types.TxKey) cacheKey {
	return cacheKey(trimToSize(key, t.maxKeyLen))
}

func trimToSize(key types.TxKey, maxKeyLen int) []byte {
	if maxKeyLen <= 0 {
		return key[:]
	}
	return key[:min(maxKeyLen, len(key))]
}
