package store

import (
	"bytes"
	"io"
	"sort"

	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
)

// FastStore is a single-goroutine giga cachekv store using plain maps instead of sync.Map.
// It MUST NOT be used from multiple goroutines concurrently.
// Use this for snapshot stores in the giga executor path where each store is
// owned by a single task goroutine.
type FastStore struct {
	cache    map[string]*types.CValue
	deleted  map[string]struct{}
	parent   types.KVStore
	storeKey types.StoreKey
}

var _ types.CacheKVStore = (*FastStore)(nil)

// NewFastStore creates a new FastStore backed by plain maps.
func NewFastStore(parent types.KVStore, storeKey types.StoreKey) *FastStore {
	return &FastStore{
		cache:    make(map[string]*types.CValue),
		deleted:  make(map[string]struct{}),
		parent:   parent,
		storeKey: storeKey,
	}
}

func (store *FastStore) GetWorkingHash() ([]byte, error) {
	panic("should never attempt to get working hash from cache kv store")
}

func (store *FastStore) GetStoreType() types.StoreType {
	return store.parent.GetStoreType()
}

func (store *FastStore) Get(key []byte) []byte {
	types.AssertValidKey(key)
	keyStr := UnsafeBytesToStr(key)
	if cv, ok := store.cache[keyStr]; ok {
		return cv.Value()
	}
	return store.parent.Get(key)
}

func (store *FastStore) Set(key []byte, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	store.setCacheValue(key, value, false, true)
}

func (store *FastStore) Has(key []byte) bool {
	return store.Get(key) != nil
}

func (store *FastStore) Delete(key []byte) {
	types.AssertValidKey(key)
	store.setCacheValue(key, nil, true, true)
}

func (store *FastStore) Write() {
	keys := make([]string, 0, len(store.cache))
	for k, v := range store.cache {
		if v.Dirty() {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		if _, del := store.deleted[key]; del {
			store.parent.Delete([]byte(key))
			continue
		}
		if cv, ok := store.cache[key]; ok && cv.Value() != nil {
			store.parent.Set([]byte(key), cv.Value())
		}
	}

	// Clear both maps — in the FastStore snapshot chain, parents are either
	// another FastStore or a VIS, both of which make writes immediately
	// visible. Reads after Write() fall through to the parent.
	clear(store.cache)
	clear(store.deleted)
}

func (store *FastStore) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return NewFastStore(store, storeKey)
}

func (store *FastStore) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return NewFastStore(tracekv.NewStore(store, w, tc), storeKey)
}

func (store *FastStore) VersionExists(version int64) bool {
	return store.parent.VersionExists(version)
}

func (store *FastStore) setCacheValue(key, value []byte, deleted bool, dirty bool) {
	types.AssertValidKey(key)
	keyStr := UnsafeBytesToStr(key)
	store.cache[keyStr] = types.NewCValue(value, dirty)
	if deleted {
		store.deleted[keyStr] = struct{}{}
	} else {
		delete(store.deleted, keyStr)
	}
}

func (store *FastStore) GetParent() types.KVStore {
	return store.parent
}

func (store *FastStore) DeleteAll(start, end []byte) error {
	for _, k := range store.GetAllKeyStrsInRange(start, end) {
		store.Delete([]byte(k))
	}
	return nil
}

func (store *FastStore) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	keyStrs := map[string]struct{}{}
	for _, pk := range store.parent.GetAllKeyStrsInRange(start, end) {
		keyStrs[pk] = struct{}{}
	}
	for k, v := range store.cache {
		kbz := []byte(k)
		if bytes.Compare(kbz, start) < 0 || bytes.Compare(kbz, end) >= 0 {
			continue
		}
		if v.Value() == nil {
			delete(keyStrs, k)
		} else {
			keyStrs[k] = struct{}{}
		}
	}
	for k := range keyStrs {
		res = append(res, k)
	}
	return res
}

func (store *FastStore) Iterator(start, end []byte) types.Iterator {
	panic("unexpected iterator call on fast giga cachekv store")
}

func (store *FastStore) ReverseIterator(start, end []byte) types.Iterator {
	panic("unexpected reverse iterator call on fast giga cachekv store")
}
