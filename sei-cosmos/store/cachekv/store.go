package cachekv

import (
	"bytes"
	"io"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-cosmos/internal/conv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/tracekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
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
	storeKey      types.StoreKey
	cacheSize     int

	// frozen marks a layer that has been superseded by a newer cache layer and
	// will therefore never receive another write via this store reference (writes
	// go to the newest layer). It is set opt-in via Freeze() — callers that never
	// call Freeze (all non-EVM code) get exactly the previous behavior.
	frozen atomic.Bool
	// dirty is set the first time a key is written (Set/Delete) and cleared on
	// Write(). A frozen layer with dirty==false is empty and, by the freeze
	// invariant, stays empty, so reads may skip it entirely.
	dirty atomic.Bool
	// readParent memoizes the nearest ancestor a read must consult, skipping any
	// run of frozen empty layers. Valid for the store's lifetime because a frozen
	// empty layer never gains writes (see readThroughParent).
	readParent atomic.Pointer[types.KVStore]
}

var _ types.CacheKVStore = (*Store)(nil)

// NewStore creates a new Store object
func NewStore(parent types.KVStore, storeKey types.StoreKey, cacheSize int) *Store {
	return &Store{
		cache:         &sync.Map{},
		deleted:       &sync.Map{},
		unsortedCache: &sync.Map{},
		sortedCache:   nil,
		parent:        parent,
		storeKey:      storeKey,
		cacheSize:     cacheSize,
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
	if cv, ok := store.cache.Load(conv.UnsafeBytesToStr(key)); ok {
		return cv.(*types.CValue).Value()
	}
	return store.readThroughParent().Get(key)
}

// readThroughParent returns the store a cache-missing read must fall through to.
// It normally returns store.parent, but when the parent is a frozen empty cache
// layer it walks up, skipping every consecutive frozen empty layer, and returns
// the first ancestor that could actually hold data. Empty layers contribute
// nothing to a point read (getFromCache falls straight through them), so the
// result is identical to walking one layer at a time — only O(1) instead of
// O(depth) once a deep stack of empty snapshot layers has accumulated.
//
// The result is safe to memoize: a layer is only skipped when it is frozen AND
// empty, and a frozen layer never gains writes (writes always target the newest,
// unfrozen layer). The single exception is RevertToSnapshot re-exposing a layer
// as the newest layer, but that discards every layer above it — including any
// store that memoized a skip over it — so no live memo can go stale.
func (store *Store) readThroughParent() types.KVStore {
	// Cheap gate: only nested cache layers can be skipped. The common single-layer
	// case (parent is an iavl/dbadapter store) never enters the skip path.
	cp, ok := store.parent.(*Store)
	if !ok || !cp.frozen.Load() || cp.dirty.Load() {
		return store.parent
	}
	if p := store.readParent.Load(); p != nil {
		return *p
	}
	p := store.parent
	for {
		next, ok := p.(*Store)
		if !ok || !next.frozen.Load() || next.dirty.Load() {
			break
		}
		p = next.parent
	}
	store.readParent.Store(&p)
	return p
}

// Freeze marks the store as superseded by a newer cache layer. After Freeze the
// caller must not write to the store again (writes go to the newer layer); this
// lets deeper layers skip it for reads while it is empty. Freeze is idempotent.
func (store *Store) Freeze() {
	store.frozen.Store(true)
}

// Unfreeze reverts Freeze, marking the store writable again. It is called when a
// layer is re-exposed as the newest layer (e.g. RevertToSnapshot), so that reads
// stop skipping it — a writable top must never be treated as a frozen empty layer.
// Deeper stores gate on the parent's live frozen bit, so unfreezing here also
// bypasses any stale memo that skipped this store while it was frozen.
func (store *Store) Unfreeze() {
	store.frozen.Store(false)
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
	store.sortedCache = nil
	// The layer is empty again; drop the memoized skip so a subsequent read
	// recomputes it against the current parent chain.
	store.dirty.Store(false)
	store.readParent.Store(nil)
}

// CacheWrap implements CacheWrapper.
func (store *Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return NewStore(store, storeKey, store.cacheSize)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (store *Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return NewStore(tracekv.NewStore(store, w, tc), storeKey, store.cacheSize)
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

func (store *Store) getOrInitSortedCache() *dbm.MemDB {
	if store.sortedCache == nil {
		store.sortedCache = dbm.NewMemDB()
	}
	return store.sortedCache
}

func (store *Store) iterator(start, end []byte, ascending bool) types.Iterator {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	// TODO: (occ) Note that for iterators, we'll need to have special handling (discussed in RFC) to ensure proper validation

	var parent, cache types.Iterator

	// Iterate the nearest ancestor that can hold data, skipping any run of frozen
	// empty layers. An empty layer has no sets and no deletes, so it contributes
	// nothing to iteration; skipping it avoids building an O(depth) chain of
	// cacheMergeIterators over a deep snapshot stack.
	parentStore := store.readThroughParent()
	if ascending {
		parent = parentStore.Iterator(start, end)
	} else {
		parent = parentStore.ReverseIterator(start, end)
	}
	defer func() {
		if err := recover(); err != nil {
			// close out parent iterator, then reraise panic
			if parent != nil {
				_ = parent.Close()
			}
			panic(err)
		}
	}()
	store.dirtyItems(start, end)
	cache = newMemIterator(start, end, store.getOrInitSortedCache(), store.deleted, ascending)
	// Fast path: when this layer has no cached writes or deletes within [start, end),
	// the merge is a pure pass-through of the parent iterator. Returning the parent
	// directly avoids wrapping every empty cache layer in a cacheMergeIterator. Deeply
	// nested cache stacks (e.g. one CacheMultiStore layer per EVM call frame) would
	// otherwise force each Value()/Next()/Valid() to recurse through O(depth) merge
	// iterators, turning a linear number of reads into quadratic work. Because empty
	// layers pass through, nested empty layers collapse the whole chain to the base
	// iterator. Note dirtyItems materializes in-range deletes into sortedCache, so a
	// non-Valid cache correctly means "no sets and no deletes in range".
	if !cache.Valid() {
		if err := cache.Close(); err != nil {
			if parent != nil {
				_ = parent.Close()
			}
			panic(err)
		}
		return parent
	}
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
			if err := store.getOrInitSortedCache().Set(item.Key, []byte{}); err != nil {
				panic(err)
			}

			continue
		}
		if err := store.getOrInitSortedCache().Set(item.Key, item.Value); err != nil {
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
	// Mark the layer non-empty so deeper layers stop skipping it for reads.
	store.dirty.Store(true)
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
