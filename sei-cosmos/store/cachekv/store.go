package cachekv

import (
	"bytes"
	"io"
	"sort"
	"sync"

	"github.com/cosmos/cosmos-sdk/internal/conv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"
	abci "github.com/tendermint/tendermint/abci/types"
	dbm "github.com/tendermint/tm-db"
)

// Store wraps an in-memory cache around an underlying types.KVStore.
type Store struct {
	mtx           sync.RWMutex
	cache         *sync.Map
	deleted       *sync.Map
	unsortedCache *sync.Map
	sortedCache   *dbm.MemDB // always ascending sorted
	parent        types.KVStore
	eventManager  *sdktypes.EventManager
	storeKey      types.StoreKey
	cacheSize     int
}

var _ types.CacheKVStore = (*Store)(nil)

// NewStore creates a new Store object
func NewStore(parent types.KVStore, storeKey types.StoreKey, cacheSize int) *Store {
	return &Store{
		cache:         &sync.Map{},
		deleted:       &sync.Map{},
		unsortedCache: &sync.Map{},
		sortedCache:   dbm.NewMemDB(),
		parent:        parent,
		eventManager:  sdktypes.NewEventManager(),
		storeKey:      storeKey,
		cacheSize:     cacheSize,
	}
}

func (store *Store) GetWorkingHash() ([]byte, error) {
	panic("should never attempt to get working hash from cache kv store")
}

// Implements Store
func (store *Store) GetEvents() []abci.Event {
	return store.eventManager.ABCIEvents()
}

// Implements Store
func (store *Store) ResetEvents() {
	store.eventManager = sdktypes.NewEventManager()
}

// GetStoreType implements Store.
func (store *Store) GetStoreType() types.StoreType {
	return store.parent.GetStoreType()
}

// getFromCache queries the write-through cache for a value by key.
func (store *Store) getFromCache(key []byte) []byte {
	if cv, ok := store.cache.Load(conv.UnsafeBytesToStr(key)); ok {
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
	store.unsortedCache = &sync.Map{}
	store.sortedCache = dbm.NewMemDB()
}

// CacheWrap implements CacheWrapper.
func (store *Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return NewStore(store, storeKey, store.cacheSize)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (store *Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return NewStore(tracekv.NewStore(store, w, tc), storeKey, store.cacheSize)
}

// CacheWrapWithListeners implements the CacheWrapper interface.
func (store *Store) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return NewStore(listenkv.NewStore(store, storeKey, listeners), storeKey, store.cacheSize)
}

//----------------------------------------
// Iteration

// Iterator implements types.KVStore.
func (store *Store) Iterator(start, end []byte) types.Iterator {
	return store.iterator(start, end, true)
}

// ReverseIterator implements types.KVStore.
func (store *Store) ReverseIterator(start, end []byte) types.Iterator {
	return store.iterator(start, end, false)
}

func (store *Store) iterator(start, end []byte, ascending bool) types.Iterator {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	// TODO: (occ) Note that for iterators, we'll need to have special handling (discussed in RFC) to ensure proper validation

	var parent, cache types.Iterator

	if ascending {
		parent = store.parent.Iterator(start, end)
	} else {
		parent = store.parent.ReverseIterator(start, end)
	}
	defer func() {
		if err := recover(); err != nil {
			// close out parent iterator, then reraise panic
			if parent != nil {
				parent.Close()
			}
			panic(err)
		}
	}()
	store.dirtyItems(start, end)
	cache = newMemIterator(start, end, store.sortedCache, store.deleted, ascending, store.eventManager, store.storeKey)
	return NewCacheMergeIterator(parent, cache, ascending, store.storeKey)
}

func (store *Store) VersionExists(version int64) bool {
	return store.parent.VersionExists(version)
}

func findStartIndex(strL []string, startQ string) int {
	// Modified binary search to find the very first element in >=startQ.
	if len(strL) == 0 {
		return -1
	}

	var left, right, mid int
	right = len(strL) - 1
	for left <= right {
		mid = (left + right) >> 1
		midStr := strL[mid]
		if midStr == startQ {
			// Handle condition where there might be multiple values equal to startQ.
			// We are looking for the very first value < midStL, that i+1 will be the first
			// element >= midStr.
			for i := mid - 1; i >= 0; i-- {
				if strL[i] != midStr {
					return i + 1
				}
			}
			return 0
		}
		if midStr < startQ {
			left = mid + 1
		} else { // midStrL > startQ
			right = mid - 1
		}
	}
	if left >= 0 && left < len(strL) && strL[left] >= startQ {
		return left
	}
	return -1
}

func findEndIndex(strL []string, endQ string) int {
	if len(strL) == 0 {
		return -1
	}

	// Modified binary search to find the very first element <endQ.
	var left, right, mid int
	right = len(strL) - 1
	for left <= right {
		mid = (left + right) >> 1
		midStr := strL[mid]
		if midStr == endQ {
			// Handle condition where there might be multiple values equal to startQ.
			// We are looking for the very first value < midStL, that i+1 will be the first
			// element >= midStr.
			for i := mid - 1; i >= 0; i-- {
				if strL[i] < midStr {
					return i + 1
				}
			}
			return 0
		}
		if midStr < endQ {
			left = mid + 1
		} else { // midStrL > startQ
			right = mid - 1
		}
	}

	// Binary search failed, now let's find a value less than endQ.
	for i := right; i >= 0; i-- {
		if strL[i] < endQ {
			return i
		}
	}

	return -1
}

type sortState int

const (
	stateUnsorted sortState = iota
	stateAlreadySorted
)

const minSortSize = 1024

// Constructs a slice of dirty items, to use w/ memIterator.
func (store *Store) dirtyItems(start, end []byte) {
	startStr, endStr := conv.UnsafeBytesToStr(start), conv.UnsafeBytesToStr(end)
	if end != nil && startStr > endStr {
		// Nothing to do here.
		return
	}

	unsorted := make([]*kv.Pair, 0)
	// If the unsortedCache is too big, its costs too much to determine
	// what's in the subset we are concerned about.
	// If you are interleaving iterator calls with writes, this can easily become an
	// O(N^2) overhead.
	// Even without that, too many range checks eventually becomes more expensive
	// than just not having the cache.
	// store.emitUnsortedCacheSizeMetric()
	store.unsortedCache.Range(func(key, value any) bool {
		cKey := key.(string)
		if dbm.IsKeyInDomain(conv.UnsafeStrToBytes(cKey), start, end) {
			cacheValue, ok := store.cache.Load(key)
			if ok {
				unsorted = append(unsorted, &kv.Pair{Key: []byte(cKey), Value: cacheValue.(*types.CValue).Value()})
			}
		}
		return true
	})
	store.clearUnsortedCacheSubset(unsorted, stateUnsorted)
	return
}

func (store *Store) clearUnsortedCacheSubset(unsorted []*kv.Pair, sortState sortState) {
	store.deleteKeysFromUnsortedCache(unsorted)

	if sortState == stateUnsorted {
		sort.Slice(unsorted, func(i, j int) bool {
			return bytes.Compare(unsorted[i].Key, unsorted[j].Key) < 0
		})
	}

	for _, item := range unsorted {
		if item.Value == nil {
			// deleted element, tracked by store.deleted
			// setting arbitrary value
			if err := store.sortedCache.Set(item.Key, []byte{}); err != nil {
				panic(err)
			}

			continue
		}
		if err := store.sortedCache.Set(item.Key, item.Value); err != nil {
			panic(err)
		}
	}
}

func (store *Store) deleteKeysFromUnsortedCache(unsorted []*kv.Pair) {
	for _, kv := range unsorted {
		keyStr := conv.UnsafeBytesToStr(kv.Key)
		store.unsortedCache.Delete(keyStr)
	}
}

//----------------------------------------
// etc

// Only entrypoint to mutate store.cache.
func (store *Store) setCacheValue(key, value []byte, deleted bool, dirty bool) {
	types.AssertValidKey(key)

	keyStr := conv.UnsafeBytesToStr(key)
	store.cache.Store(keyStr, types.NewCValue(value, dirty))
	if deleted {
		store.deleted.Store(keyStr, struct{}{})
	} else {
		store.deleted.Delete(keyStr)
	}
	if dirty {
		store.unsortedCache.Store(keyStr, struct{}{})
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
