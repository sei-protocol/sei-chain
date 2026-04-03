package interblock

import (
	"bytes"
	"io"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

// Cache is a KVStore that persists across block boundaries. It is a
// write-through read cache: every Set/Delete propagates to the parent
// CommitKVStore immediately, so the parent is always authoritative and visible
// to all readers (including the v2/legacy execution phase that follows the
// giga execution phase within the same block). The in-memory map accelerates
// reads by serving recently-written and recently-read keys from memory rather
// than hitting the on-disk CommitKVStore, and this warmth survives across
// block boundaries.
//
// FlushDirty is a no-op because the parent is always current; it exists only
// to satisfy the InterBlockCache interface. UpdateParent must be called after
// each Commit because the underlying CommitKVStore may be reloaded.
type Cache struct {
	mtx      sync.RWMutex
	cache    *sync.Map // string key -> *types.CValue (always clean; parent is authoritative)
	parent   types.KVStore
	storeKey types.StoreKey
}

var _ types.InterBlockCache = (*Cache)(nil)

func NewCache(parent types.KVStore, storeKey types.StoreKey) *Cache {
	return &Cache{
		cache:    &sync.Map{},
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

// FlushDirty is a no-op for a write-through cache: every Set/Delete already
// propagated to the parent store immediately, so there is nothing to flush.
// Kept to satisfy the InterBlockCache interface.
func (c *Cache) FlushDirty() {}

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

// Set implements types.KVStore. Write-through: updates both the in-memory
// cache and the parent CommitKVStore so the write is immediately visible to
// all readers, including the v2/legacy execution phase that runs after the
// giga phase within the same block.
func (c *Cache) Set(key, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	c.cache.Store(string(key), types.NewCValue(value, false))
	c.mtx.RLock()
	c.parent.Set(key, value)
	c.mtx.RUnlock()
}

// Delete implements types.KVStore. Write-through: removes from both the
// in-memory cache and the parent CommitKVStore immediately.
func (c *Cache) Delete(key []byte) {
	types.AssertValidKey(key)
	c.cache.Store(string(key), types.NewCValue(nil, false))
	c.mtx.RLock()
	c.parent.Delete(key)
	c.mtx.RUnlock()
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
