package store

import (
	"io"
	"time"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sstypes "github.com/sei-protocol/sei-db/ss/types"
)

const StoreTypeSeiStateStore = 100

var _ types.KVStore = (*Store)(nil)

// Store wraps a SS store and implements a cosmos KVStore
type Store struct {
	store    sstypes.StateStore
	storeKey types.StoreKey
	version  int64
}

func NewKVStore(store sstypes.StateStore, storeKey types.StoreKey, version int64) *Store {
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
	value, err := st.store.Get(st.storeKey.Name(), st.version, key)
	if err != nil {
		panic(err)
	}
	return value
}

func (st *Store) Has(key []byte) bool {
	defer telemetry.MeasureSince(time.Now(), "store", "state-store", "has")
	has, err := st.store.Has(st.storeKey.Name(), st.version, key)
	if err != nil {
		panic(err)
	}
	return has
}

func (st *Store) Set(_, _ []byte) {
	panic("write operation is not supported")
}

func (st *Store) Delete(_ []byte) {
	panic("write operation is not supported")
}

func (st *Store) Iterator(start, end []byte) types.Iterator {
	itr, err := st.store.Iterator(st.storeKey.Name(), st.version, start, end)
	if err != nil {
		panic(err)
	}
	return itr
}

func (st *Store) ReverseIterator(start, end []byte) types.Iterator {
	itr, err := st.store.ReverseIterator(st.storeKey.Name(), st.version, start, end)
	if err != nil {
		panic(err)
	}
	return itr
}

func (st *Store) GetWorkingHash() ([]byte, error) {
	panic("get working hash operation is not supported")
}
