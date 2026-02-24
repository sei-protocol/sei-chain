package store

import (
	"bytes"
	"io"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/tracekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

// Store wraps an in-memory cache around an underlying types.KVStore.
type Store struct {
	mtx       sync.RWMutex
	cache     *sync.Map
	deleted   *sync.Map
	parent    types.KVStore
	storeKey  types.StoreKey
	cacheSize int
}

var _ types.CacheKVStore = (*Store)(nil)

// NewStore creates a new Store object
func NewStore(parent types.KVStore, storeKey types.StoreKey, cacheSize int) *Store {
	return &Store{
		cache:     &sync.Map{},
		deleted:   &sync.Map{},
		parent:    parent,
		storeKey:  storeKey,
		cacheSize: cacheSize,
	}
}

func (store *Store) GetWorkingHash() ([]byte, error) {
	panic("should never attempt to get working hash from cache kv store")
}

// GetStoreType implements Store.
func (store *Store) GetStoreType() types.StoreType {
	return store.parent.GetStoreType()
}

// getFromCache queries the write-through cache for a value by key.
func (store *Store) getFromCache(key []byte) []byte {
	if cv, ok := store.cache.Load(UnsafeBytesToStr(key)); ok {
		return cv.(*types.CValue).Value()
	}
	return store.parent.Get(key)
}

// Get implements types.KVStore.
func (store *Store) Get(key []byte) (value []byte) {
	types.AssertValidKey(key)
	return store.getFromCache(key)
}

// Set implements types.KVStore.
func (store *Store) Set(key []byte, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	store.setCacheValue(key, value, false, true)
}

// Has implements types.KVStore.
func (store *Store) Has(key []byte) bool {
	value := store.Get(key)
	return value != nil
}

// Delete implements types.KVStore.
func (store *Store) Delete(key []byte) {
	types.AssertValidKey(key)
	store.setCacheValue(key, nil, true, true)
}

// Implements Cachetypes.KVStore.
func (store *Store) Write() {
	store.mtx.Lock()
	defer store.mtx.Unlock()

	// We need a copy of all of the keys.
	// Not the best, but probably not a bottleneck depending.
	keys := []string{}

	store.cache.Range(func(key, value any) bool {
		if value.(*types.CValue).Dirty() {
			keys = append(keys, key.(string))
		}
		return true
	})
	sort.Strings(keys)
	// TODO: Consider allowing usage of Batch, which would allow the write to
	// at least happen atomically.
	for _, key := range keys {
		if store.isDeleted(key) {
			// We use []byte(key) instead of conv.UnsafeStrToBytes because we cannot
			// be sure if the underlying store might do a save with the byteslice or
			// not. Once we get confirmation that .Delete is guaranteed not to
			// save the byteslice, then we can assume only a read-only copy is sufficient.
			store.parent.Delete([]byte(key))
			continue
		}

		cacheValue, ok := store.cache.Load(key)
		if ok && cacheValue.(*types.CValue).Value() != nil {
			// It already exists in the parent, hence delete it.
			store.parent.Set([]byte(key), cacheValue.(*types.CValue).Value())
		}
	}

	store.cache = &sync.Map{}
	store.deleted = &sync.Map{}
}

// CacheWrap implements CacheWrapper.
func (store *Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return NewStore(store, storeKey, store.cacheSize)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (store *Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return NewStore(tracekv.NewStore(store, w, tc), storeKey, store.cacheSize)
}

func (store *Store) VersionExists(version int64) bool {
	return store.parent.VersionExists(version)
}

// Only entrypoint to mutate store.cache.
func (store *Store) setCacheValue(key, value []byte, deleted bool, dirty bool) {
	types.AssertValidKey(key)

	keyStr := UnsafeBytesToStr(key)
	store.cache.Store(keyStr, types.NewCValue(value, dirty))
	if deleted {
		store.deleted.Store(keyStr, struct{}{})
	} else {
		store.deleted.Delete(keyStr)
	}
}

func (store *Store) isDeleted(key string) bool {
	_, ok := store.deleted.Load(key)
	return ok
}

func (store *Store) GetParent() types.KVStore {
	return store.parent
}

func (store *Store) DeleteAll(start, end []byte) error {
	for _, k := range store.GetAllKeyStrsInRange(start, end) {
		store.Delete([]byte(k))
	}
	return nil
}

func (store *Store) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	keyStrs := map[string]struct{}{}
	for _, pk := range store.parent.GetAllKeyStrsInRange(start, end) {
		keyStrs[pk] = struct{}{}
	}
	store.cache.Range(func(key, value any) bool {
		kbz := []byte(key.(string))
		if bytes.Compare(kbz, start) < 0 || bytes.Compare(kbz, end) >= 0 {
			// we don't want to break out of the iteration since cache isn't sorted
			return true
		}
		cv := value.(*types.CValue)
		if cv.Value() == nil {
			delete(keyStrs, key.(string))
		} else {
			keyStrs[key.(string)] = struct{}{}
		}
		return true
	})
	for k := range keyStrs {
		res = append(res, k)
	}
	return res
}

func (store *Store) Iterator(start, end []byte) types.Iterator {
	panic("unexpected iterator call on cachekv store")
}

// ReverseIterator implements types.KVStore.
// Stub: delegates to parent store reverse iterator (minimal implementation to satisfy interface)
func (store *Store) ReverseIterator(start, end []byte) types.Iterator {
	panic("unexpected reverse iterator call on cachekv store")
}
