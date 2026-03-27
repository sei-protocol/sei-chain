package interblock

import (
	"bytes"
	"io"
	"sort"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/types"
)

// Cache is a KVStore that persists across block boundaries. It wraps an
// underlying parent KVStore and maintains an in-memory write-through cache.
// Only entries written since the last FlushDirty are dirty; FlushDirty writes
// them to the parent and marks them clean, leaving all other cached entries
// untouched. This gives O(dirty) flush cost rather than O(total cache size).
type Cache struct {
	mtx      sync.RWMutex
	cache    *sync.Map // string key -> *types.CValue
	deleted  *sync.Map // tracks keys pending deletion
	parent   types.KVStore
	storeKey types.StoreKey
}

var _ types.InterBlockCache = (*Cache)(nil)

func NewCache(parent types.KVStore, storeKey types.StoreKey) *Cache {
	return &Cache{
		cache:    &sync.Map{},
		deleted:  &sync.Map{},
		parent:   parent,
		storeKey: storeKey,
	}
}

// UpdateParent refreshes the parent store reference. Must be called at block
// start because the underlying CommitKVStore may be reloaded after Commit.
func (c *Cache) UpdateParent(parent types.KVStore) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.parent = parent
}

// FlushDirty writes only entries modified since the last flush to the parent
// store, then marks them clean. O(dirty entries), not O(total cache size).
func (c *Cache) FlushDirty() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	var dirtyKeys []string
	c.cache.Range(func(key, value any) bool {
		if value.(*types.CValue).Dirty() {
			dirtyKeys = append(dirtyKeys, key.(string))
		}
		return true
	})
	sort.Strings(dirtyKeys)

	for _, key := range dirtyKeys {
		if _, isDeleted := c.deleted.Load(key); isDeleted {
			c.parent.Delete([]byte(key))
		} else if cv, ok := c.cache.Load(key); ok {
			if v := cv.(*types.CValue).Value(); v != nil {
				c.parent.Set([]byte(key), v)
			}
		}
		// Mark as clean so it stays in cache for subsequent reads.
		if cv, ok := c.cache.Load(key); ok {
			c.cache.Store(key, types.NewCValue(cv.(*types.CValue).Value(), false))
		}
	}
	c.deleted = &sync.Map{}
}

// Get implements types.KVStore.
func (c *Cache) Get(key []byte) []byte {
	types.AssertValidKey(key)
	if cv, ok := c.cache.Load(string(key)); ok {
		return cv.(*types.CValue).Value()
	}
	c.mtx.RLock()
	val := c.parent.Get(key)
	c.mtx.RUnlock()
	// Populate the cache on miss so subsequent reads of the same key stay
	// in memory across transactions and blocks (analogous to sei-v3's
	// CacheKVStore populate-on-miss). Only non-nil values are cached; nil
	// means the key is absent in the parent and we don't want to mask a
	// future write from another path.
	if val != nil {
		c.cache.Store(string(key), types.NewCValue(val, false))
	}
	return val
}

// Has implements types.KVStore.
func (c *Cache) Has(key []byte) bool {
	return c.Get(key) != nil
}

// Set implements types.KVStore.
func (c *Cache) Set(key, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	keyStr := string(key)
	c.cache.Store(keyStr, types.NewCValue(value, true))
	c.deleted.Delete(keyStr)
}

// Delete implements types.KVStore.
func (c *Cache) Delete(key []byte) {
	types.AssertValidKey(key)
	keyStr := string(key)
	c.cache.Store(keyStr, types.NewCValue(nil, true))
	c.deleted.Store(keyStr, struct{}{})
}

// DeleteAll implements types.KVStore.
func (c *Cache) DeleteAll(start, end []byte) error {
	for _, k := range c.GetAllKeyStrsInRange(start, end) {
		c.Delete([]byte(k))
	}
	return nil
}

// GetAllKeyStrsInRange implements types.KVStore.
func (c *Cache) GetAllKeyStrsInRange(start, end []byte) []string {
	c.mtx.RLock()
	parentKeys := c.parent.GetAllKeyStrsInRange(start, end)
	c.mtx.RUnlock()

	keySet := make(map[string]struct{}, len(parentKeys))
	for _, k := range parentKeys {
		keySet[k] = struct{}{}
	}
	c.cache.Range(func(key, value any) bool {
		kb := []byte(key.(string))
		if bytes.Compare(kb, start) < 0 || bytes.Compare(kb, end) >= 0 {
			return true
		}
		if value.(*types.CValue).Value() == nil {
			delete(keySet, key.(string))
		} else {
			keySet[key.(string)] = struct{}{}
		}
		return true
	})
	result := make([]string, 0, len(keySet))
	for k := range keySet {
		result = append(result, k)
	}
	return result
}

// GetStoreType implements types.Store.
func (c *Cache) GetStoreType() types.StoreType {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.parent.GetStoreType()
}

// CacheWrap implements types.Store.
func (c *Cache) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	panic("CacheWrap not supported on InterBlockCache")
}

// CacheWrapWithTrace implements types.Store.
func (c *Cache) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	panic("CacheWrapWithTrace not supported on InterBlockCache")
}

// GetWorkingHash implements types.KVStore.
func (c *Cache) GetWorkingHash() ([]byte, error) {
	panic("GetWorkingHash not supported on InterBlockCache")
}

// VersionExists implements types.KVStore.
func (c *Cache) VersionExists(version int64) bool {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.parent.VersionExists(version)
}

// Iterator implements types.KVStore.
func (c *Cache) Iterator(start, end []byte) types.Iterator {
	panic("Iterator not supported on InterBlockCache")
}

// ReverseIterator implements types.KVStore.
func (c *Cache) ReverseIterator(start, end []byte) types.Iterator {
	panic("ReverseIterator not supported on InterBlockCache")
}
