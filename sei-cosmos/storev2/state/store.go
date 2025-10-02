package state

import (
	"fmt"
	"io"

	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/kv"
	sstypes "github.com/sei-protocol/sei-db/ss/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

const StoreTypeSSStore = 100

var (
	_ types.KVStore   = (*Store)(nil)
	_ types.Queryable = (*Store)(nil)
)

// Store wraps a SS store and implements a cosmos KVStore
type Store struct {
	store    sstypes.StateStore
	storeKey types.StoreKey
	version  int64
}

func NewStore(store sstypes.StateStore, storeKey types.StoreKey, version int64) *Store {
	return &Store{store, storeKey, version}
}

func (st *Store) GetStoreType() types.StoreType {
	return StoreTypeSSStore
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
	value, err := st.store.Get(st.storeKey.Name(), st.version, key)
	if err != nil {
		panic(err)
	}
	return value
}

func (st *Store) Has(key []byte) bool {
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

func (st *Store) Query(req abci.RequestQuery) (res abci.ResponseQuery) {
	if req.Height > 0 && req.Height > st.version {
		return sdkerrors.QueryResult(errors.Wrap(sdkerrors.ErrInvalidHeight, "invalid height"))
	}
	res.Height = st.version
	switch req.Path {
	case "/key": // get by key
		res.Key = req.Data // data holds the key bytes
		res.Value = st.Get(res.Key)
	case "/subspace":
		pairs := kv.Pairs{
			Pairs: make([]kv.Pair, 0),
		}
		subspace := req.Data
		res.Key = subspace
		iterator := types.KVStorePrefixIterator(st, subspace)
		for ; iterator.Valid(); iterator.Next() {
			pairs.Pairs = append(pairs.Pairs, kv.Pair{Key: iterator.Key(), Value: iterator.Value()})
		}
		iterator.Close()

		bz, err := pairs.Marshal()
		if err != nil {
			panic(fmt.Errorf("failed to marshal KV pairs: %w", err))
		}
		res.Value = bz
	default:
		return sdkerrors.QueryResult(errors.Wrapf(sdkerrors.ErrUnknownRequest, "unexpected query path: %v", req.Path))
	}

	return res
}

func (st *Store) VersionExists(version int64) bool {
	earliest, err := st.store.GetEarliestVersion()
	if err != nil {
		panic(err)
	}
	return version >= earliest
}

func (st *Store) DeleteAll(start, end []byte) error {
	iter := st.Iterator(start, end)
	keys := [][]byte{}
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	iter.Close()
	for _, key := range keys {
		st.Delete(key)
	}
	return nil
}

func (st *Store) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	iter := st.Iterator(start, end)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		res = append(res, string(iter.Key()))
	}
	return
}
