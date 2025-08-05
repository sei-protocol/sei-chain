package cache

import (
	"fmt"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/types"

	lru "github.com/hashicorp/golang-lru/v2"
)

var (
	_ types.CommitKVStore             = (*CommitKVStoreCache)(nil)
	_ types.MultiStorePersistentCache = (*CommitKVStoreCacheManager)(nil)

	// DefaultCommitKVStoreCacheSize defines the persistent ARC cache size for a
	// CommitKVStoreCache.
	DefaultCommitKVStoreCacheSize uint = 100000
)

type (
	// CommitKVStoreCache implements an inter-block (persistent) cache that wraps a
	// CommitKVStore. Reads first hit the internal ARC (Adaptive Replacement Cache).
	// During a cache miss, the read is delegated to the underlying CommitKVStore
	// and cached. Deletes and writes always happen to both the cache and the
	// CommitKVStore in a write-through manner. Caching performed in the
	// CommitKVStore and below is completely irrelevant to this layer.
	CommitKVStoreCache struct {
		types.CommitKVStore
		cache       *lru.TwoQueueCache[string, []byte]
		cacheKVSize int

		// the same CommitKVStoreCache may be accessed concurrently by multiple
		// goroutines due to transaction parallelization
		mtx sync.RWMutex
	}

	// CommitKVStoreCacheManager maintains a mapping from a StoreKey to a
	// CommitKVStoreCache. Each CommitKVStore, per StoreKey, is meant to be used
	// in an inter-block (persistent) manner and typically provided by a
	// CommitMultiStore.
	CommitKVStoreCacheManager struct {
		cacheSize   uint
		caches      map[string]types.CommitKVStore
		cacheKVSize int
	}
)

func NewCommitKVStoreCache(store types.CommitKVStore, size uint, cacheKVSize int) *CommitKVStoreCache {
	cache, err := lru.New2Q[string, []byte](int(size))
	if err != nil {
		panic(fmt.Errorf("failed to create KVStore cache: %s", err))
	}

	return &CommitKVStoreCache{
		CommitKVStore: store,
		cache:         cache,
		cacheKVSize:   cacheKVSize,
	}
}

func NewCommitKVStoreCacheManager(size uint, cacheKVSize int) *CommitKVStoreCacheManager {
	return &CommitKVStoreCacheManager{
		cacheSize:   size,
		caches:      make(map[string]types.CommitKVStore),
		cacheKVSize: cacheKVSize,
	}
}

// GetStoreCache returns a Cache from the CommitStoreCacheManager for a given
// StoreKey. If no Cache exists for the StoreKey, then one is created and set.
// The returned Cache is meant to be used in a persistent manner.
func (cmgr *CommitKVStoreCacheManager) GetStoreCache(key types.StoreKey, store types.CommitKVStore) types.CommitKVStore {
	if cmgr.caches[key.Name()] == nil {
		cmgr.caches[key.Name()] = NewCommitKVStoreCache(store, cmgr.cacheSize, cmgr.cacheKVSize)
	}

	return cmgr.caches[key.Name()]
}

// Unwrap returns the underlying CommitKVStore for a given StoreKey.
func (cmgr *CommitKVStoreCacheManager) Unwrap(key types.StoreKey) types.CommitKVStore {
	if ckv, ok := cmgr.caches[key.Name()]; ok {
		return ckv.(*CommitKVStoreCache).CommitKVStore
	}

	return nil
}

// Reset resets in the internal caches.
func (cmgr *CommitKVStoreCacheManager) Reset() {
	for _, ckv := range cmgr.caches {
		// not deleting CommitKVStoreCache themselves from the manager to prevent
		// Unwrap returning nil
		ckv.(*CommitKVStoreCache).Reset()
	}
}

// CacheWrap implements the CacheWrapper interface
func (ckv *CommitKVStoreCache) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return cachekv.NewStore(ckv, storeKey, ckv.cacheKVSize)
}

// getFromCache queries the write-through cache for a value by key.
func (ckv *CommitKVStoreCache) getFromCache(key []byte) ([]byte, bool) {
	ckv.mtx.RLock()
	defer ckv.mtx.RUnlock()
	return ckv.cache.Get(string(key))
}

// getAndWriteToCache queries the underlying CommitKVStore and writes the result
func (ckv *CommitKVStoreCache) getAndWriteToCache(key []byte) []byte {
	ckv.mtx.RLock()
	defer ckv.mtx.RUnlock()
	value := ckv.CommitKVStore.Get(key)
	ckv.cache.Add(string(key), value)
	return value
}

// Get retrieves a value by key. It will first look in the write-through cache.
// If the value doesn't exist in the write-through cache, the query is delegated
// to the underlying CommitKVStore.
func (ckv *CommitKVStoreCache) Get(key []byte) []byte {
	types.AssertValidKey(key)

	if value, ok := ckv.getFromCache(key); ok {
		return value
	}

	// if not found in the cache, query the underlying CommitKVStore and init cache value
	return ckv.getAndWriteToCache(key)
}

// Set inserts a key/value pair into both the write-through cache and the
// underlying CommitKVStore.
func (ckv *CommitKVStoreCache) Set(key, value []byte) {
	ckv.mtx.Lock()
	defer ckv.mtx.Unlock()

	types.AssertValidKey(key)
	types.AssertValidValue(value)

	ckv.cache.Add(string(key), value)
	ckv.CommitKVStore.Set(key, value)
}

// Delete removes a key/value pair from both the write-through cache and the
// underlying CommitKVStore.
func (ckv *CommitKVStoreCache) Delete(key []byte) {
	ckv.mtx.Lock()
	defer ckv.mtx.Unlock()

	ckv.cache.Remove(string(key))
	ckv.CommitKVStore.Delete(key)
}

func (ckv *CommitKVStoreCache) Reset() {
	ckv.mtx.Lock()
	defer ckv.mtx.Unlock()

	ckv.cache.Purge()
}
