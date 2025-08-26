package mempool

import (
	"container/list"
	"fmt"
	"github.com/rs/zerolog/log"
	"sync"
	"time"

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

	// Push adds the given raw transaction to the cache and returns true if it was
	// newly added. Otherwise, it returns false.
	Push(tx types.Tx) bool

	// Remove removes the given raw transaction from the cache.
	Remove(tx types.Tx)

	// Size returns the current size of the cache
	Size() int
}

var _ TxCache = (*LRUTxCache)(nil)

// LRUTxCache maintains a thread-safe LRU cache of raw transactions. The cache
// only stores the hash of the raw transaction.
type LRUTxCache struct {
	mtx      sync.Mutex
	size     int
	cacheMap map[types.TxKey]*list.Element
	list     *list.List
}

func NewLRUTxCache(cacheSize int) *LRUTxCache {
	return &LRUTxCache{
		size:     cacheSize,
		cacheMap: make(map[types.TxKey]*list.Element, cacheSize),
		list:     list.New(),
	}
}

// GetList returns the underlying linked-list that backs the LRU cache. Note,
// this should be used for testing purposes only!
func (c *LRUTxCache) GetList() *list.List {
	return c.list
}

func (c *LRUTxCache) Reset() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.cacheMap = make(map[types.TxKey]*list.Element, c.size)
	c.list.Init()
}

func (c *LRUTxCache) Push(tx types.Tx) bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	key := tx.Key()

	moved, ok := c.cacheMap[key]
	if ok {
		c.list.MoveToBack(moved)
		return false
	}

	if c.list.Len() >= c.size {
		front := c.list.Front()
		if front != nil {
			frontKey := front.Value.(types.TxKey)
			delete(c.cacheMap, frontKey)
			c.list.Remove(front)
		}
	}

	e := c.list.PushBack(key)
	c.cacheMap[key] = e

	return true
}

func (c *LRUTxCache) Remove(tx types.Tx) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	key := tx.Key()
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

// NopTxCache defines a no-op raw transaction cache.
type NopTxCache struct{}

var _ TxCache = (*NopTxCache)(nil)

func (NopTxCache) Reset()             {}
func (NopTxCache) Push(types.Tx) bool { return true }
func (NopTxCache) Remove(types.Tx)    {}
func (NopTxCache) Size() int          { return 0 }

// NopTxCacheWithTTL defines a no-op TTL transaction cache.
type NopTxCacheWithTTL struct{}

var _ TxCacheWithTTL = (*NopTxCacheWithTTL)(nil)

func (NopTxCacheWithTTL) Set(_ types.TxKey, _ int)                    {}
func (NopTxCacheWithTTL) Get(_ types.TxKey) (counter int, found bool) { return 0, false }
func (NopTxCacheWithTTL) Increment(_ types.TxKey)                     {}
func (NopTxCacheWithTTL) Reset()                                      {}

func (NopTxCacheWithTTL) GetForMetrics() (int, int, int, int) { return 0, 0, 0, 0 }

func (NopTxCacheWithTTL) Stop() {}

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
	maxSize int
	cache   *cache.Cache
}

// NewDuplicateTxCache creates a new TTL transaction cache
func NewDuplicateTxCache(maxSize int, defaultExpiration, cleanupInterval time.Duration) *DuplicateTxCache {
	// If defaultExpiration is 0 (no expiration), don't create a cleanup interval
	// to avoid starting background janitor goroutines that can cause leaks
	if defaultExpiration == 0 {
		cleanupInterval = 0
		log.Debug().Msg("TTL cache expiration disabled")
	}

	return &DuplicateTxCache{
		maxSize: maxSize,
		cache:   cache.New(defaultExpiration, cleanupInterval),
	}
}

// Set adds a transaction to the cache with TTL
func (t *DuplicateTxCache) Set(txKey types.TxKey, counter int) {
	t.cache.SetDefault(txKeyToString(txKey), counter)
}

// Get retrieves the counter for a transaction key
func (t *DuplicateTxCache) Get(txKey types.TxKey) (counter int, found bool) {
	if value, exists := t.cache.Get(txKeyToString(txKey)); exists {
		if counter, ok := value.(int); ok {
			return counter, true
		}
	}
	return 0, false
}

// Increment increments the counter for a transaction key, extending TTL
func (t *DuplicateTxCache) Increment(txKey types.TxKey) {
	key := txKeyToString(txKey)
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

// txKeyToString converts a TxKey (byte array) to a stable hex string.
func txKeyToString(txKey types.TxKey) string {
	return fmt.Sprintf("%x", txKey)
}
