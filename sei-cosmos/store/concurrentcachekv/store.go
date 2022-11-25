package cachekv

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/internal/conv"
	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"
	abci "github.com/tendermint/tendermint/abci/types"
	dbm "github.com/tendermint/tm-db"
)

// If value is nil but deleted is false, it means the parent doesn't have the
// key.  (No need to delete upon Write())
type cValue struct {
	value []byte
	dirty bool
}

// Store wraps an in-memory cache around an underlying types.KVStore.
// Concurrent Safe Version of CacheKV
type Store struct {
	mtx           sync.Mutex
	cache         *sync.Map
	deleted       *sync.Map
	unsortedCache *sync.Map
	sortedCache   *dbm.MemDB // always ascending sorted
	parent        types.KVStore
	eventManager  *sdktypes.EventManager
	storeKey	  types.StoreKey
}

var _ types.CacheKVStore = (*Store)(nil)

// NewStore creates a new Store object
func NewStore(parent types.KVStore, storeKey types.StoreKey) *Store {
	return &Store{
		sortedCache:   dbm.NewMemDB(),
		parent:        parent,
		eventManager:  sdktypes.NewEventManager(),
		storeKey: 	   storeKey,
	}
}

func (store *Store) GetWorkingHash() []byte {
	panic("should never attempt to get working hash from cache kv store")
}

// Implements Store
func (store *Store) GetEvents() []abci.Event {
	return store.eventManager.ABCIEvents()
}

// GetStoreType implements Store.
func (store *Store) GetStoreType() types.StoreType {
	return store.parent.GetStoreType()
}

// Get implements types.KVStore.
func (store *Store) Get(key []byte) (value []byte) {
	store.mtx.Lock()
	defer store.mtx.Unlock()

	types.AssertValidKey(key)

	val, ok := store.cache.Load(conv.UnsafeBytesToStr(key))
	cacheValue := val.(*cValue)
	if !ok {
		value = store.parent.Get(key)
		store.setCacheValue(key, value, false, false)
	} else {
		value = cacheValue.value
	}
	store.eventManager.EmitResourceAccessReadEvent("get", store.storeKey, key, value)

	return value
}

// Set implements types.KVStore.
func (store *Store) Set(key []byte, value []byte) {
	store.mtx.Lock()
	defer store.mtx.Unlock()

	types.AssertValidKey(key)
	types.AssertValidValue(value)

	store.setCacheValue(key, value, false, true)
	store.eventManager.EmitResourceAccessWriteEvent("set", store.storeKey, key, value)
}

// Has implements types.KVStore.
func (store *Store) Has(key []byte) bool {
	value := store.Get(key)
	store.eventManager.EmitResourceAccessReadEvent("has", store.storeKey, key, value)
	return value != nil
}

// Delete implements types.KVStore.
func (store *Store) Delete(key []byte) {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "cachekv", "delete")

	types.AssertValidKey(key)
	store.setCacheValue(key, nil, true, true)
	store.eventManager.EmitResourceAccessReadEvent("delete", store.storeKey, key, []byte{})
}

// Implements Cachetypes.KVStore.
func (store *Store) Write() {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "cachekv", "write")

	// We need a copy of all of the keys.
	// Not the best, but probably not a bottleneck depending.
	keys := make([]string, 0)

	store.cache.Range(func(key, value any) bool {
		stringKey := key.(string)
		cacheValue := value.(*cValue)
		if cacheValue.dirty {
			keys = append(keys, stringKey)
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

		value, ok := store.cache.Load(key)
		if !ok {
			panic(fmt.Sprintf("Cache Store key=%s should exist in ConcurrentCacheKV store", key))
		}
		cacheValue := value.(*cValue)
		if cacheValue.value != nil {
			// It already exists in the parent, hence delete it.
			store.parent.Set([]byte(key), cacheValue.value)
		}
	}

	// Clear the cache using the map clearing idiom
	// and not allocating fresh objects.
	// Please see https://bencher.orijtech.com/perfclinic/mapclearing/
	store.cache.Range(func(key, value any) bool {
		store.cache.Delete(key)
		return true
	})

	store.deleted.Range(func(key, value any) bool {
		store.deleted.Delete(key)
		return true
	})

	store.unsortedCache.Range(func(key, value any) bool {
		store.unsortedCache.Delete(key)
		return true
	})
	store.sortedCache = dbm.NewMemDB()
}

// CacheWrap implements CacheWrapper.
func (store *Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return NewStore(store, storeKey)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (store *Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return NewStore(tracekv.NewStore(store, w, tc), storeKey)
}

// CacheWrapWithListeners implements the CacheWrapper interface.
func (store *Store) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return NewStore(listenkv.NewStore(store, storeKey, listeners), storeKey)
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

	var parent, cache types.Iterator

	if ascending {
		parent = store.parent.Iterator(start, end)
	} else {
		parent = store.parent.ReverseIterator(start, end)
	}

	store.dirtyItems(start, end)
	cache = newMemIterator(start, end, store.sortedCache, store.deleted, ascending, store.eventManager, store.storeKey)

	return cachekv.NewCacheMergeIterator(parent, cache, ascending)
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

// Constructs a slice of dirty items, to use w/ memIterator.
func (store *Store) dirtyItems(start, end []byte) {
	startStr, endStr := conv.UnsafeBytesToStr(start), conv.UnsafeBytesToStr(end)
	if startStr > endStr {
		// Nothing to do here.
		return
	}

	numUnsortedCache := 0
	store.unsortedCache.Range(func(key, value any) bool {
		numUnsortedCache += 1
		return true
	})
	unsorted := make([]*kv.Pair, 0)
	// If the unsortedCache is too big, its costs too much to determine
	// whats in the subset we are concerned about.
	// If you are interleaving iterator calls with writes, this can easily become an
	// O(N^2) overhead.
	// Even without that, too many range checks eventually becomes more expensive
	// than just not having the cache.
	if numUnsortedCache < 1024 {
		store.unsortedCache.Range(func(key, value any) bool {
			stringKey := key.(string)
			if dbm.IsKeyInDomain(conv.UnsafeStrToBytes(stringKey), start, end) {
				value, ok := store.cache.Load(key)
				if !ok {
					panic(fmt.Sprintf("key=%s should exist in ConcurrentCacheKv", stringKey))
				}
				cacheValue := value.(*cValue)
				unsorted = append(unsorted, &kv.Pair{Key: []byte(stringKey), Value: cacheValue.value})
			}
			return true
		})
		store.clearUnsortedCacheSubset(unsorted, stateUnsorted)
		return
	}

	// Otherwise it is large so perform a modified binary search to find
	// the target ranges for the keys that we should be looking for.
	strL := make([]string, 0, numUnsortedCache)
	store.unsortedCache.Range(func(key, value any) bool {
		stringKey := key.(string)
		strL = append(strL, stringKey)
		return true
	})
	sort.Strings(strL)

	// Now find the values within the domain
	//  [start, end)
	startIndex := findStartIndex(strL, startStr)
	endIndex := findEndIndex(strL, endStr)

	if endIndex < 0 {
		endIndex = len(strL) - 1
	}
	if startIndex < 0 {
		startIndex = 0
	}

	kvL := make([]*kv.Pair, 0)
	for i := startIndex; i <= endIndex; i++ {
		key := strL[i]
		value,  ok := store.cache.Load(key)
		if !ok {
			panic(fmt.Sprintf("key=%s should exist in ConcurrentCacheKv", key))
		}
		cacheValue := value.(*cValue)
		kvL = append(kvL, &kv.Pair{Key: []byte(key), Value: cacheValue.value})
	}

	// kvL was already sorted so pass it in as is.
	store.clearUnsortedCacheSubset(kvL, stateAlreadySorted)
}

func (store *Store) clearUnsortedCacheSubset(unsorted []*kv.Pair, sortState sortState) {
	store.unsortedCache.Range(func(key, value any) bool {
		store.unsortedCache.Delete(key)
		return true
	})

	if sortState == stateUnsorted {
		sort.Slice(unsorted, func(i, j int) bool {
			return bytes.Compare(unsorted[i].Key, unsorted[j].Key) < 0
		})
	}

	for _, item := range unsorted {
		if item.Value == nil {
			// deleted element, tracked by store.deleted
			// setting arbitrary value
			store.sortedCache.Set(item.Key, []byte{})
			continue
		}
		err := store.sortedCache.Set(item.Key, item.Value)
		if err != nil {
			panic(err)
		}
	}
}

//----------------------------------------
// etc

// Only entrypoint to mutate store.cache.
func (store *Store) setCacheValue(key, value []byte, deleted bool, dirty bool) {
	keyStr := conv.UnsafeBytesToStr(key)
	store.cache.Store(
		keyStr,
		&cValue{ value: value, dirty: dirty},
	)
	if deleted {
		store.deleted.Store(keyStr, struct{}{})
	} else {
		store.deleted.Delete(keyStr)
	}
	if dirty {
		store.unsortedCache.Store(conv.UnsafeBytesToStr(key), struct{}{})
	}
}

func (store *Store) isDeleted(key string) bool {
	_, ok := store.deleted.Load(key)
	return ok
}
