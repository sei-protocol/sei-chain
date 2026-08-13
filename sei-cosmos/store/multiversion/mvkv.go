package multiversion

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/store/types"
	scheduler "github.com/cosmos/cosmos-sdk/types/occ"
	dbm "github.com/tendermint/tm-db"
)

// exposes a handler for adding items to readset, useful for iterators
type ReadsetHandler interface {
	UpdateReadSet(key []byte, value []byte)
}

type NoOpHandler struct{}

func (NoOpHandler) UpdateReadSet(key []byte, value []byte) {}

// exposes a handler for adding items to iterateset, to be called upon iterator close
type IterateSetHandler interface {
	UpdateIterateSet(*iterationTracker)
}

type iterationTracker struct {
	startKey     []byte              // start of the iteration range
	endKey       []byte              // end of the iteration range
	earlyStopKey []byte              // key that caused early stop
	iteratedKeys map[string]struct{} // TODO: is a map okay because the ordering will be enforced when we replay the iterator?
	ascending    bool

	writeset WriteSet

	// TODO: is it possible that terimation is affected by keys later in iteration that weren't reached? eg. number of keys affecting iteration?
	// TODO: i believe to get number of keys the iteration would need to be done fully so its not a concern?

	// TODO: maybe we need to store keys served from writeset for the transaction? that way if theres OTHER keys within the writeset and the iteration range, and were written to the writeset later, we can discriminate between the groups?
	// keysServedFromWriteset map[string]struct{}

	// actually its simpler to just store a copy of the writeset at the time of iterator creation
}

func NewIterationTracker(startKey, endKey []byte, ascending bool, writeset WriteSet) iterationTracker {
	copyWriteset := make(WriteSet, len(writeset))

	for key, value := range writeset {
		copyWriteset[key] = value
	}

	return iterationTracker{
		startKey:     startKey,
		endKey:       endKey,
		iteratedKeys: make(map[string]struct{}),
		ascending:    ascending,
		writeset:     copyWriteset,
	}
}

// AddKey adds a key to the iterated keys map and sets the early stop key as the key since it's the latest key iterated
func (item *iterationTracker) AddKey(key []byte) {
	item.iteratedKeys[string(key)] = struct{}{}
	item.SetEarlyStopKey(key)
}

func (item *iterationTracker) SetEarlyStopKey(key []byte) {
	item.earlyStopKey = key
}

// Version Indexed Store wraps the multiversion store in a way that implements the KVStore interface, but also stores the index of the transaction, and so store actions are applied to the multiversion store using that index
type VersionIndexedStore struct {
	// TODO: this shouldnt NEED a mutex because its used within single transaction execution, therefore no concurrency
	// mtx sync.Mutex
	// used for tracking reads and writes for eventual validation + persistence into multi-version store
	// TODO: does this need sync.Map?
	readset    map[string][][]byte // contains the key -> []value mapping for all keys read from the store (not mvkv, underlying store)
	writeset   map[string][]byte   // contains the key -> value mapping for all keys written to the store
	iterateset Iterateset
	// TODO: need to add iterateset here as well

	// used for iterators - populated at the time of iterator instantiation
	// TODO: when we want to perform iteration, we need to move all the dirty keys (writeset and readset) into the sortedTree and then combine with the iterators for the underlying stores
	sortedStore *dbm.MemDB // always ascending sorted
	// parent stores (both multiversion and underlying parent store)
	multiVersionStore MultiVersionStore
	parent            types.KVStore
	// transaction metadata for versioned operations
	transactionIndex int
	incarnation      int
	// have abort channel here for aborting transactions
	abortChannel chan scheduler.Abort
}

var _ types.KVStore = (*VersionIndexedStore)(nil)
var _ ReadsetHandler = (*VersionIndexedStore)(nil)
var _ IterateSetHandler = (*VersionIndexedStore)(nil)

func NewVersionIndexedStore(parent types.KVStore, multiVersionStore MultiVersionStore, transactionIndex, incarnation int, abortChannel chan scheduler.Abort) *VersionIndexedStore {
	return &VersionIndexedStore{
		readset:           make(map[string][][]byte),
		writeset:          make(map[string][]byte),
		iterateset:        []*iterationTracker{},
		sortedStore:       dbm.NewMemDB(),
		parent:            parent,
		multiVersionStore: multiVersionStore,
		transactionIndex:  transactionIndex,
		incarnation:       incarnation,
		abortChannel:      abortChannel,
	}
}

// GetReadset returns the readset
func (store *VersionIndexedStore) GetReadset() map[string][][]byte {
	return store.readset
}

// GetWriteset returns the writeset
func (store *VersionIndexedStore) GetWriteset() map[string][]byte {
	return store.writeset
}

// WriteAbort writes an abort to the store but only allows one abort to be written PER instance of mvkv. This is because we pair abort channel writes with panics, and if we hit this more than once, it means that the panic was swallowed, so we won't write any aborts after a first abort is written to prevent any potential for deadlocking due to full channels
func (store *VersionIndexedStore) WriteAbort(abort scheduler.Abort) {
	select {
	case store.abortChannel <- abort:
	default:
		fmt.Println("WARN: abort channel full, discarding val")
	}
}

// Get implements types.KVStore.
func (store *VersionIndexedStore) Get(key []byte) []byte {
	// first try to get from writeset cache, if cache miss, then try to get from multiversion store, if that misses, then get from parent store
	// if the key is in the cache, return it

	// don't have RW mutex because we have to update readset
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()
	// defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "get")

	types.AssertValidKey(key)
	strKey := string(key)
	// first check the MVKV writeset, and return that value if present
	cacheValue, ok := store.writeset[strKey]
	if ok {
		// return the value from the cache, no need to update any readset stuff
		return cacheValue
	}
	// read the readset to see if the value exists - and return if applicable
	if readsetVal, ok := store.readset[strKey]; ok {
		// just return the first one, if there is more than one, we will fail the validation anyways
		return readsetVal[0]
	}

	// if we didn't find it, then we want to check the multivalue store + add to readset if applicable
	mvsValue := store.multiVersionStore.GetLatestBeforeIndex(store.transactionIndex, key)
	if mvsValue != nil {
		if mvsValue.IsEstimate() {
			abort := scheduler.NewEstimateAbort(mvsValue.Index())
			store.WriteAbort(abort)
			panic(abort)
		} else {
			// This handles both detecting readset conflicts and updating readset if applicable
			return store.parseValueAndUpdateReadset(strKey, mvsValue)
		}
	}
	// if we didn't find it in the multiversion store, then we want to check the parent store + add to readset
	parentValue := store.parent.Get(key)
	store.UpdateReadSet(key, parentValue)
	return parentValue
}

// This functions handles reads with deleted items and values and verifies that the data is consistent to what we currently have in the readset (IF we have a readset value for that key)
func (store *VersionIndexedStore) parseValueAndUpdateReadset(strKey string, mvsValue MultiVersionValueItem) []byte {
	value := mvsValue.Value()
	if mvsValue.IsDeleted() {
		value = nil
	}
	store.UpdateReadSet([]byte(strKey), value)
	return value
}

// This function iterates over the readset, validating that the values in the readset are consistent with the values in the multiversion store and underlying parent store, and returns a boolean indicating validity
func (store *VersionIndexedStore) ValidateReadset() bool {
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()
	// defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "validate_readset")

	// sort the readset keys - this is so we have consistent behavior when theres varying conflicts within the readset (eg. read conflict vs estimate)
	readsetKeys := make([]string, 0, len(store.readset))
	for key := range store.readset {
		readsetKeys = append(readsetKeys, key)
	}
	sort.Strings(readsetKeys)

	// iterate over readset keys and values
	for _, strKey := range readsetKeys {
		key := []byte(strKey)
		valueArr := store.readset[strKey]
		if len(valueArr) != 1 {
			// if we have more than one value, we will fail the validation since we dedup when adding to readset
			return false
		}
		value := valueArr[0]
		mvsValue := store.multiVersionStore.GetLatestBeforeIndex(store.transactionIndex, key)
		if mvsValue != nil {
			if mvsValue.IsEstimate() {
				// if we see an estimate, that means that we need to abort and rerun
				store.WriteAbort(scheduler.NewEstimateAbort(mvsValue.Index()))
				return false
			} else {
				if mvsValue.IsDeleted() {
					// check for `nil`
					if value != nil {
						return false
					}
				} else {
					// check for equality
					if string(value) != string(mvsValue.Value()) {
						return false
					}
				}
			}
			continue // value is valid, continue to next key
		}

		parentValue := store.parent.Get(key)
		if string(parentValue) != string(value) {
			// this shouldnt happen because if we have a conflict it should always happen within multiversion store
			panic("we shouldn't ever have a readset conflict in parent store")
		}
		// value was correct, we can continue to the next value
	}
	return true
}

// Delete implements types.KVStore.
func (store *VersionIndexedStore) Delete(key []byte) {
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()
	// defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "delete")

	types.AssertValidKey(key)
	store.setValue(key, nil)
}

// Has implements types.KVStore.
func (store *VersionIndexedStore) Has(key []byte) bool {
	// necessary locking happens within store.Get
	return store.Get(key) != nil
}

// Set implements types.KVStore.
func (store *VersionIndexedStore) Set(key []byte, value []byte) {
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()
	// defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "set")

	types.AssertValidKey(key)
	store.setValue(key, value)
}

// Iterator implements types.KVStore.
func (v *VersionIndexedStore) Iterator(start []byte, end []byte) dbm.Iterator {
	return v.iterator(start, end, true)
}

// ReverseIterator implements types.KVStore.
func (v *VersionIndexedStore) ReverseIterator(start []byte, end []byte) dbm.Iterator {
	return v.iterator(start, end, false)
}

// Iterator implements types.KVStore.
func (store *VersionIndexedStore) iterator(start []byte, end []byte, ascending bool) dbm.Iterator {
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()

	// get the sorted keys from MVS
	// TODO: ideally we take advantage of mvs keys already being sorted
	// TODO: ideally merge btree and mvs keys into a single sorted btree
	memDB := store.multiVersionStore.CollectIteratorItems(store.transactionIndex)

	// TODO: ideally we persist writeset keys into a sorted btree for later use
	// make a set of total keys across mvkv and mvs to iterate
	for key := range store.writeset {
		memDB.Set([]byte(key), []byte{})
	}
	// also add readset elements such that they fetch from readset instead of parent
	for key := range store.readset {
		memDB.Set([]byte(key), []byte{})
	}

	var parent, memIterator types.Iterator

	// make a memIterator
	memIterator = store.newMemIterator(start, end, memDB, ascending)

	if ascending {
		parent = store.parent.Iterator(start, end)
	} else {
		parent = store.parent.ReverseIterator(start, end)
	}

	mergeIterator := NewMVSMergeIterator(parent, memIterator, ascending, store)

	iterationTracker := NewIterationTracker(start, end, ascending, store.writeset)
	store.UpdateIterateSet(&iterationTracker)
	trackedIterator := NewTrackedIterator(mergeIterator, &iterationTracker)

	// mergeIterator
	return trackedIterator

}

func (v *VersionIndexedStore) VersionExists(version int64) bool {
	return v.parent.VersionExists(version)
}

func (v *VersionIndexedStore) DeleteAll(start, end []byte) error {
	for _, k := range v.GetAllKeyStrsInRange(start, end) {
		v.Delete([]byte(k))
	}
	return nil
}

func (v *VersionIndexedStore) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	iter := v.Iterator(start, end)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		res = append(res, string(iter.Key()))
	}
	return
}

// GetStoreType implements types.KVStore.
func (v *VersionIndexedStore) GetStoreType() types.StoreType {
	return v.parent.GetStoreType()
}

// CacheWrap implements types.KVStore.
func (*VersionIndexedStore) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	panic("CacheWrap not supported for version indexed store")
}

// CacheWrapWithListeners implements types.KVStore.
func (*VersionIndexedStore) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	panic("CacheWrapWithListeners not supported for version indexed store")
}

// CacheWrapWithTrace implements types.KVStore.
func (*VersionIndexedStore) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	panic("CacheWrapWithTrace not supported for version indexed store")
}

// GetWorkingHash implements types.KVStore.
func (v *VersionIndexedStore) GetWorkingHash() ([]byte, error) {
	panic("should never attempt to get working hash from version indexed store")
}

// Only entrypoint to mutate writeset
func (store *VersionIndexedStore) setValue(key, value []byte) {
	types.AssertValidKey(key)

	keyStr := string(key)
	store.writeset[keyStr] = value
}

func (store *VersionIndexedStore) WriteToMultiVersionStore() {
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()
	// defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "write_mvs")
	store.multiVersionStore.SetWriteset(store.transactionIndex, store.incarnation, store.writeset)
	store.multiVersionStore.SetReadset(store.transactionIndex, store.readset)
	store.multiVersionStore.SetIterateset(store.transactionIndex, store.iterateset)
}

func (store *VersionIndexedStore) WriteEstimatesToMultiVersionStore() {
	// TODO: remove?
	// store.mtx.Lock()
	// defer store.mtx.Unlock()
	// defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "write_mvs")
	store.multiVersionStore.SetEstimatedWriteset(store.transactionIndex, store.incarnation, store.writeset)
	// TODO: do we need to write readset and iterateset in this case? I don't think so since if this is called it means we aren't doing validation
}

func (store *VersionIndexedStore) UpdateReadSet(key []byte, value []byte) {
	// TODO: make readset a list of byte slices, and store the value if it's a new value
	// add to readset
	keyStr := string(key)
	// TODO: maybe only add if not already existing?
	if _, ok := store.readset[keyStr]; !ok {
		// if the entry doesnt exist, make a new empty slice
		store.readset[keyStr] = [][]byte{}
	}
	for _, readsetVal := range store.readset[keyStr] {
		if bytes.Equal(value, readsetVal) {
			// this means we have already added this value to our readset, so we continue
			return
		}
	}
	// if we get here, that means we have a new readset val, so we append it to the slice
	store.readset[keyStr] = append(store.readset[keyStr], value)
}

// Write implements types.CacheWrap so this store can exist on the cache multi store
func (store *VersionIndexedStore) Write() {
	panic("not implemented")
}

// GetEvents implements types.CacheWrap so this store can exist on the cache multi store
func (store *VersionIndexedStore) GetEvents() []abci.Event {
	panic("not implemented")
}

// ResetEvents implements types.CacheWrap so this store can exist on the cache multi store
func (store *VersionIndexedStore) ResetEvents() {
	panic("not implemented")
}

func (store *VersionIndexedStore) UpdateIterateSet(iterationTracker *iterationTracker) {
	// TODO: refactor such that the iterateset is added to the store at the time of iterator creation and updated continuously instead of at Close
	// append to iterateset
	store.iterateset = append(store.iterateset, iterationTracker)
}
