package dbadapter

import (
	"io"

	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
)

// Wrapper type for dbm.Db with implementation of KVStore
type Store struct {
	dbm.DB
}

func (dsa Store) GetWorkingHash() ([]byte, error) {
	return []byte{}, nil
}

// Get wraps the underlying DB's Get method panicing on error.
func (dsa Store) Get(key []byte) []byte {
	v, err := dsa.DB.Get(key)
	if err != nil {
		panic(err)
	}

	return v
}

// Has wraps the underlying DB's Has method panicing on error.
func (dsa Store) Has(key []byte) bool {
	ok, err := dsa.DB.Has(key)
	if err != nil {
		panic(err)
	}

	return ok
}

// Set wraps the underlying DB's Set method panicing on error.
func (dsa Store) Set(key, value []byte) {
	types.AssertValidKey(key)
	if err := dsa.DB.Set(key, value); err != nil {
		panic(err)
	}
}

// Delete wraps the underlying DB's Delete method panicing on error.
func (dsa Store) Delete(key []byte) {
	if err := dsa.DB.Delete(key); err != nil {
		panic(err)
	}
}

// Iterator wraps the underlying DB's Iterator method panicing on error.
func (dsa Store) Iterator(start, end []byte) types.Iterator {
	iter, err := dsa.DB.Iterator(start, end)
	if err != nil {
		panic(err)
	}

	return iter
}

// ReverseIterator wraps the underlying DB's ReverseIterator method panicing on error.
func (dsa Store) ReverseIterator(start, end []byte) types.Iterator {
	iter, err := dsa.DB.ReverseIterator(start, end)
	if err != nil {
		panic(err)
	}

	return iter
}

// GetStoreType returns the type of the store.
func (Store) GetStoreType() types.StoreType {
	return types.StoreTypeDB
}

// CacheWrap branches the underlying store.
func (dsa Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return cachekv.NewStore(dsa, storeKey, types.DefaultCacheSizeLimit)
}

// CacheWrapWithTrace implements KVStore.
func (dsa Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return cachekv.NewStore(tracekv.NewStore(dsa, w, tc), storeKey, types.DefaultCacheSizeLimit)
}

// CacheWrapWithListeners implements the CacheWrapper interface.
func (dsa Store) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return cachekv.NewStore(listenkv.NewStore(dsa, storeKey, listeners), storeKey, types.DefaultCacheSizeLimit)
}

func (dsa Store) VersionExists(version int64) bool {
	panic("no versioning for dbadater")
}

func (dsa Store) DeleteAll(start, end []byte) error {
	iter := dsa.Iterator(start, end)
	keys := [][]byte{}
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	iter.Close()
	for _, key := range keys {
		dsa.Delete(key)
	}
	return nil
}

func (dsa Store) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	iter := dsa.Iterator(start, end)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		res = append(res, string(iter.Key()))
	}
	return
}

// dbm.DB implements KVStore so we can CacheKVStore it.
var _ types.KVStore = Store{}
