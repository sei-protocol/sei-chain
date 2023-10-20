package ss

import (
	"io"
	"time"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

const StoreTypeSeiStateStore = 100

var _ types.KVStore = (*Store)(nil)

// Store wraps a SS store and implements a cosmos KVStore
type Store struct {
	store    StateStore
	storeKey types.StoreKey
	version  int64
}

func NewKVStore(store StateStore, storeKey types.StoreKey, version int64) *Store {
	return &Store{store, storeKey, version}
}

func (st *Store) GetStoreType() types.StoreType {
	return StoreTypeSeiStateStore
}

func (st *Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return cachekv.NewStore(st, storeKey, types.DefaultCacheSizeLimit)
}

func (st *Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return cachekv.NewStore(tracekv.NewStore(st, w, tc), storeKey, types.DefaultCacheSizeLimit)
}

func (st *Store) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return cachekv.NewStore(listenkv.NewStore(st, storeKey, listeners), storeKey, types.DefaultCacheSizeLimit)
}

func (st *Store) Get(key []byte) []byte {
	defer telemetry.MeasureSince(time.Now(), "store", "state-store", "get")
	value, err := st.store.GetAtVersion(st.storeKey.Name(), key, st.version)
	if err != nil {
		panic(err)
	}
	return value
}

func (st *Store) Has(key []byte) bool {
	defer telemetry.MeasureSince(time.Now(), "store", "versiondb", "has")
	has, err := st.store.HasAtVersion(st.storeKey.Name(), key, st.version)
	if err != nil {
		panic(err)
	}
	return has
}

func (st *Store) Set(key, value []byte) {
	panic("write operation is not supported")
}

func (st *Store) Delete(key []byte) {
	panic("write operation is not supported")
}

func (st *Store) Iterator(start, end []byte) types.Iterator {
	itr, err := st.store.Iterator(st.storeKey.Name(), start, end, st.version)
	if err != nil {
		panic(err)
	}
	return itr
}

func (st *Store) ReverseIterator(start, end []byte) types.Iterator {
	itr, err := st.store.ReverseIterator(st.storeKey.Name(), start, end, st.version)
	if err != nil {
		panic(err)
	}
	return itr
}

func (st *Store) GetWorkingHash() ([]byte, error) {
	panic("get working hash operation is not supported")
}
