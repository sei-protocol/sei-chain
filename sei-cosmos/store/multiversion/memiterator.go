package multiversion

import (
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/store/types"
	occtypes "github.com/cosmos/cosmos-sdk/types/occ"
)

// Iterates over iterKVCache items.
// if key is nil, means it was deleted.
// Implements Iterator.
type memIterator struct {
	types.Iterator
	mvkv *VersionIndexedStore
}

func (store *VersionIndexedStore) newMemIterator(
	start, end []byte,
	items *dbm.MemDB,
	ascending bool,
) *memIterator {
	var iter types.Iterator
	var err error

	if ascending {
		iter, err = items.Iterator(start, end)
	} else {
		iter, err = items.ReverseIterator(start, end)
	}

	if err != nil {
		if iter != nil {
			iter.Close()
		}
		panic(err)
	}

	return &memIterator{
		Iterator: iter,
		mvkv:     store,
	}
}

// try to get value from the writeset, otherwise try to get from multiversion store, otherwise try to get from parent
func (mi *memIterator) Value() []byte {
	key := mi.Iterator.Key()
	// TODO: verify that this is correct
	return mi.mvkv.Get(key)
}

type validationIterator struct {
	types.Iterator

	mvStore      MultiVersionStore
	writeset     WriteSet
	index        int
	abortChannel chan occtypes.Abort

	// this ensure that we serve consistent values throughout the lifecycle of the validationIterator - this should prevent race conditions causing an iterator to become invalid while being used
	readCache map[string][]byte
}

func (store *Store) newMVSValidationIterator(
	index int,
	start, end []byte,
	items *dbm.MemDB,
	ascending bool,
	writeset WriteSet,
	abortChannel chan occtypes.Abort,
) *validationIterator {
	var iter types.Iterator
	var err error

	if ascending {
		iter, err = items.Iterator(start, end)
	} else {
		iter, err = items.ReverseIterator(start, end)
	}

	if err != nil {
		if iter != nil {
			iter.Close()
		}
		panic(err)
	}

	return &validationIterator{
		Iterator:     iter,
		mvStore:      store,
		index:        index,
		abortChannel: abortChannel,
		writeset:     writeset,
		readCache:    make(map[string][]byte),
	}
}

// try to get value from the writeset, otherwise try to get from multiversion store, otherwise try to get from parent iterator
func (vi *validationIterator) Value() []byte {
	key := vi.Iterator.Key()

	// try fetch from writeset - return if exists
	if val, ok := vi.writeset[string(key)]; ok {
		return val
	}
	// serve value from readcache (means it has previously been accessed by this iterator so we want consistent behavior here)
	if val, ok := vi.readCache[string(key)]; ok {
		return val
	}

	// get the value from the multiversion store
	val := vi.mvStore.GetLatestBeforeIndex(vi.index, key)

	// if we have an estimate, write to abort channel
	if val.IsEstimate() {
		vi.abortChannel <- occtypes.NewEstimateAbort(val.Index())
	}

	// if we have a deleted value, return nil
	if val.IsDeleted() {
		vi.readCache[string(key)] = nil
		return nil
	}
	vi.readCache[string(key)] = val.Value()
	return val.Value()
}
