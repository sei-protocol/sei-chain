package multiversion

import (
	"io"
	"sort"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	scheduler "github.com/cosmos/cosmos-sdk/types/occ"
	dbm "github.com/tendermint/tm-db"
)

// Version Indexed Store wraps the multiversion store in a way that implements the KVStore interface, but also stores the index of the transaction, and so store actions are applied to the multiversion store using that index
type VersionIndexedStore struct {
	mtx sync.Mutex
	// used for tracking reads and writes for eventual validation + persistence into multi-version store
	readset  map[string][]byte // contains the key -> value mapping for all keys read from the store (not mvkv, underlying store)
	writeset map[string][]byte // contains the key -> value mapping for all keys written to the store
	// TODO: need to add iterateset here as well

	// dirty keys that haven't been sorted yet for iteration
	dirtySet map[string]struct{}
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

func NewVersionIndexedStore(parent types.KVStore, multiVersionStore MultiVersionStore, transactionIndex, incarnation int, abortChannel chan scheduler.Abort) *VersionIndexedStore {
	return &VersionIndexedStore{
		readset:           make(map[string][]byte),
		writeset:          make(map[string][]byte),
		dirtySet:          make(map[string]struct{}),
		sortedStore:       dbm.NewMemDB(),
		parent:            parent,
		multiVersionStore: multiVersionStore,
		transactionIndex:  transactionIndex,
		incarnation:       incarnation,
		abortChannel:      abortChannel,
	}
}

// GetReadset returns the readset
func (store *VersionIndexedStore) GetReadset() map[string][]byte {
	return store.readset
}

// GetWriteset returns the writeset
func (store *VersionIndexedStore) GetWriteset() map[string][]byte {
	return store.writeset
}

// Get implements types.KVStore.
func (store *VersionIndexedStore) Get(key []byte) []byte {
	// first try to get from writeset cache, if cache miss, then try to get from multiversion store, if that misses, then get from parent store
	// if the key is in the cache, return it

	// don't have RW mutex because we have to update readset
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "get")

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
		return readsetVal
	}

	// if we didn't find it, then we want to check the multivalue store + add to readset if applicable
	mvsValue := store.multiVersionStore.GetLatestBeforeIndex(store.transactionIndex, key)
	if mvsValue != nil {
		if mvsValue.IsEstimate() {
			store.abortChannel <- scheduler.NewEstimateAbort(mvsValue.Index())
			return nil
		} else {
			// This handles both detecting readset conflicts and updating readset if applicable
			return store.parseValueAndUpdateReadset(strKey, mvsValue)
		}
	}
	// if we didn't find it in the multiversion store, then we want to check the parent store + add to readset
	parentValue := store.parent.Get(key)
	store.updateReadSet(key, parentValue)
	return parentValue
}

// This functions handles reads with deleted items and values and verifies that the data is consistent to what we currently have in the readset (IF we have a readset value for that key)
func (store *VersionIndexedStore) parseValueAndUpdateReadset(strKey string, mvsValue MultiVersionValueItem) []byte {
	value := mvsValue.Value()
	if mvsValue.IsDeleted() {
		value = nil
	}
	store.updateReadSet([]byte(strKey), value)
	return value
}

// This function iterates over the readset, validating that the values in the readset are consistent with the values in the multiversion store and underlying parent store, and returns a boolean indicating validity
func (store *VersionIndexedStore) ValidateReadset() bool {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "validate_readset")

	// sort the readset keys - this is so we have consistent behavior when theres varying conflicts within the readset (eg. read conflict vs estimate)
	readsetKeys := make([]string, 0, len(store.readset))
	for key := range store.readset {
		readsetKeys = append(readsetKeys, key)
	}
	sort.Strings(readsetKeys)

	// iterate over readset keys and values
	for _, strKey := range readsetKeys {
		key := []byte(strKey)
		value := store.readset[strKey]
		mvsValue := store.multiVersionStore.GetLatestBeforeIndex(store.transactionIndex, key)
		if mvsValue != nil {
			if mvsValue.IsEstimate() {
				// if we see an estimate, that means that we need to abort and rerun
				store.abortChannel <- scheduler.NewEstimateAbort(mvsValue.Index())
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
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "delete")

	types.AssertValidKey(key)
	store.setValue(key, nil, true, true)
}

// Has implements types.KVStore.
func (store *VersionIndexedStore) Has(key []byte) bool {
	// necessary locking happens within store.Get
	return store.Get(key) != nil
}

// Set implements types.KVStore.
func (store *VersionIndexedStore) Set(key []byte, value []byte) {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "set")

	types.AssertValidKey(key)
	store.setValue(key, value, false, true)
}

// Iterator implements types.KVStore.
func (v *VersionIndexedStore) Iterator(start []byte, end []byte) dbm.Iterator {
	return v.iterator(start, end, true)
}

// ReverseIterator implements types.KVStore.
func (v *VersionIndexedStore) ReverseIterator(start []byte, end []byte) dbm.Iterator {
	return v.iterator(start, end, false)
}

// TODO: still needs iterateset tracking
// Iterator implements types.KVStore.
func (store *VersionIndexedStore) iterator(start []byte, end []byte, ascending bool) dbm.Iterator {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	// TODO: ideally we persist writeset keys into a sorted btree for later use
	// make a set of total keys across mvkv and mvs to iterate
	keysToIterate := make(map[string]struct{})
	for key := range store.writeset {
		keysToIterate[key] = struct{}{}
	}

	// TODO: ideally we take advantage of mvs keys already being sorted
	// get the multiversion store sorted keys
	writesetMap := store.multiVersionStore.GetAllWritesetKeys()
	for i := 0; i < store.transactionIndex; i++ {
		// add all the writesets keys up until current index
		for _, key := range writesetMap[i] {
			keysToIterate[key] = struct{}{}
		}
	}
	// TODO: ideally merge btree and mvs keys into a single sorted btree

	// TODO: this is horribly inefficient, fix this
	sortedKeys := make([]string, len(keysToIterate))
	for key := range keysToIterate {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	memDB := dbm.NewMemDB()
	for _, key := range sortedKeys {
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

	// mergeIterator
	return NewMVSMergeIterator(parent, memIterator, ascending)

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
func (store *VersionIndexedStore) setValue(key, value []byte, deleted bool, dirty bool) {
	types.AssertValidKey(key)

	keyStr := string(key)
	store.writeset[keyStr] = value
	if dirty {
		store.dirtySet[keyStr] = struct{}{}
	}
}

func (store *VersionIndexedStore) WriteToMultiVersionStore() {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "write_mvs")
	store.multiVersionStore.SetWriteset(store.transactionIndex, store.incarnation, store.writeset)
}

func (store *VersionIndexedStore) WriteEstimatesToMultiVersionStore() {
	store.mtx.Lock()
	defer store.mtx.Unlock()
	defer telemetry.MeasureSince(time.Now(), "store", "mvkv", "write_mvs")
	store.multiVersionStore.SetEstimatedWriteset(store.transactionIndex, store.incarnation, store.writeset)
}

func (store *VersionIndexedStore) updateReadSet(key []byte, value []byte) {
	// add to readset
	keyStr := string(key)
	store.readset[keyStr] = value
	// add to dirty set
	store.dirtySet[keyStr] = struct{}{}
}
