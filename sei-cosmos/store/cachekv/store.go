package cachekv

import (
	"bytes"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/internal/conv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/math"
	dbm "github.com/tendermint/tm-db"
)

type mapCacheBackend struct {
	m map[string]*types.CValue
}

func (b mapCacheBackend) Get(key string) (val *types.CValue, ok bool) {
	val, ok = b.m[key]
	return
}

func (b mapCacheBackend) Set(key string, val *types.CValue) {
	b.m[key] = val
}

func (b mapCacheBackend) Len() int {
	return len(b.m)
}

func (b mapCacheBackend) Delete(key string) {
	delete(b.m, key)
}

func (b mapCacheBackend) Range(f func(string, *types.CValue) bool) {
	// this is always called within a mutex so all operations below are atomic
	keys := []string{}
	for k := range b.m {
		keys = append(keys, k)
	}
	for _, key := range keys {
		val, _ := b.Get(key)
		if !f(key, val) {
			break
		}
	}
}

// Store wraps an in-memory cache around an underlying types.KVStore.
type Store struct {
	mtx           sync.Mutex
	cache         *types.BoundedCache
	deleted       *sync.Map
	unsortedCache map[string]struct{}
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
		cache:         types.NewBoundedCache(mapCacheBackend{make(map[string]*types.CValue)}, cacheSize),
		deleted:       &sync.Map{},
		unsortedCache: make(map[string]struct{}),
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
	store.mtx.Lock()
	defer store.mtx.Unlock()
	store.eventManager = sdktypes.NewEventManager()
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

	cacheValue, ok := store.cache.Get(conv.UnsafeBytesToStr(key))
	if !ok {
		value = store.parent.Get(key)
		store.setCacheValue(key, value, false, false)
	} else {
		value = cacheValue.Value()
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
	store.mtx.Lock()
	defer store.mtx.Unlock()
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
	store.eventManager.EmitResourceAccessWriteEvent("delete", store.storeKey, key, []byte{})
}

// Implements Cachetypes.KVStore.
func (store *Store) Write() {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "cachekv", "write")

	// We need a copy of all of the keys.
	// Not the best, but probably not a bottleneck depending.
	keys := make([]string, 0, store.cache.Len())

	store.cache.Range(func(key string, dbValue *types.CValue) bool {
		if dbValue.Dirty() {
			keys = append(keys, key)
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

		cacheValue, _ := store.cache.Get(key)
		if cacheValue.Value() != nil {
			// It already exists in the parent, hence delete it.
			store.parent.Set([]byte(key), cacheValue.Value())
		}
	}

	// Clear the cache using the map clearing idiom
	// and not allocating fresh objects.
	// Please see https://bencher.orijtech.com/perfclinic/mapclearing/
	store.cache.DeleteAll()
	store.deleted.Range(func(key, value any) bool {
		store.deleted.Delete(key)
		return true
	})
	for key := range store.unsortedCache {
		delete(store.unsortedCache, key)
	}
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
	return NewCacheMergeIterator(parent, cache, ascending, store.eventManager, store.storeKey)
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

	n := len(store.unsortedCache)
	unsorted := make([]*kv.Pair, 0)
	// If the unsortedCache is too big, its costs too much to determine
	// whats in the subset we are concerned about.
	// If you are interleaving iterator calls with writes, this can easily become an
	// O(N^2) overhead.
	// Even without that, too many range checks eventually becomes more expensive
	// than just not having the cache.
	store.emitUnsortedCacheSizeMetric()
	if n < minSortSize {
		for key := range store.unsortedCache {
			if dbm.IsKeyInDomain(conv.UnsafeStrToBytes(key), start, end) {
				cacheValue, _ := store.cache.Get(key)
				unsorted = append(unsorted, &kv.Pair{Key: []byte(key), Value: cacheValue.Value()})
			}
		}
		store.clearUnsortedCacheSubset(unsorted, stateUnsorted)
		return
	}

	// Otherwise it is large so perform a modified binary search to find
	// the target ranges for the keys that we should be looking for.
	strL := make([]string, 0, n)
	for key := range store.unsortedCache {
		strL = append(strL, key)
	}
	sort.Strings(strL)

	startIndex, endIndex := findStartEndIndex(strL, startStr, endStr)

	// Since we spent cycles to sort the values, we should process and remove a reasonable amount
	// ensure start to end is at least minSortSize in size
	// if below minSortSize, expand it to cover additional values
	// this amortizes the cost of processing elements across multiple calls
	if endIndex-startIndex < minSortSize {
		endIndex = math.MinInt(startIndex+minSortSize, len(strL)-1)
		if endIndex-startIndex < minSortSize {
			startIndex = math.MaxInt(endIndex-minSortSize, 0)
		}
	}

	kvL := make([]*kv.Pair, 0, 1+endIndex-startIndex)
	for i := startIndex; i <= endIndex; i++ {
		key := strL[i]
		cacheValue, _ := store.cache.Get(key)
		kvL = append(kvL, &kv.Pair{Key: []byte(key), Value: cacheValue.Value()})
	}

	// kvL was already sorted so pass it in as is.
	store.clearUnsortedCacheSubset(kvL, stateAlreadySorted)
	store.emitUnsortedCacheSizeMetric()
}

func (store *Store) emitUnsortedCacheSizeMetric() {
	n := len(store.unsortedCache)
	telemetry.SetGauge(float32(n), "sei", "cosmos", "unsorted", "cache", "size")
}

func findStartEndIndex(strL []string, startStr, endStr string) (int, int) {
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
	return startIndex, endIndex
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
	n := len(store.unsortedCache)
	store.emitUnsortedCacheSizeMetric()
	if len(unsorted) == n { // This pattern allows the Go compiler to emit the map clearing idiom for the entire map.
		for key := range store.unsortedCache {
			delete(store.unsortedCache, key)
		}
	} else { // Otherwise, normally delete the unsorted keys from the map.
		for _, kv := range unsorted {
			delete(store.unsortedCache, conv.UnsafeBytesToStr(kv.Key))
		}
	}
	defer store.emitUnsortedCacheSizeMetric()
}

//----------------------------------------
// etc

// Only entrypoint to mutate store.cache.
func (store *Store) setCacheValue(key, value []byte, deleted bool, dirty bool) {
	types.AssertValidKey(key)

	keyStr := conv.UnsafeBytesToStr(key)
	store.cache.Set(keyStr, types.NewCValue(value, dirty))
	if deleted {
		store.deleted.Store(keyStr, struct{}{})
	} else {
		store.deleted.Delete(keyStr)
	}
	if dirty {
		store.unsortedCache[keyStr] = struct{}{}
	}
}

func (store *Store) isDeleted(key string) bool {
	_, ok := store.deleted.Load(key)
	return ok
}
