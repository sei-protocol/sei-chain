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

	mvStore      MultiVersionStore
	writeset     WriteSet
	index        int
	abortChannel chan occtypes.Abort
	ReadsetHandler
}

func (store *VersionIndexedStore) newMemIterator(
	start, end []byte,
	items *dbm.MemDB,
	ascending bool,
	readsetHandler ReadsetHandler,
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
		Iterator:       iter,
		mvStore:        store.multiVersionStore,
		index:          store.transactionIndex,
		abortChannel:   store.abortChannel,
		writeset:       store.GetWriteset(),
		ReadsetHandler: readsetHandler,
	}
}

// try to get value from the writeset, otherwise try to get from multiversion store, otherwise try to get from parent iterator
func (mi *memIterator) Value() []byte {
	key := mi.Iterator.Key()

	// try fetch from writeset - return if exists
	if val, ok := mi.writeset[string(key)]; ok {
		return val
	}

	// get the value from the multiversion store
	val := mi.mvStore.GetLatestBeforeIndex(mi.index, key)

	// if we have an estiamte, write to abort channel
	if val.IsEstimate() {
		mi.abortChannel <- occtypes.NewEstimateAbort(val.Index())
	}

	// need to update readset
	// if we have a deleted value, return nil
	if val.IsDeleted() {
		defer mi.ReadsetHandler.UpdateReadSet(key, nil)
		return nil
	}
	defer mi.ReadsetHandler.UpdateReadSet(key, val.Value())
	return val.Value()
}

func (store *Store) newMVSValidationIterator(
	index int,
	start, end []byte,
	items *dbm.MemDB,
	ascending bool,
	writeset WriteSet,
	abortChannel chan occtypes.Abort,
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
		Iterator:       iter,
		mvStore:        store,
		index:          index,
		abortChannel:   abortChannel,
		ReadsetHandler: NoOpHandler{},
		writeset:       writeset,
	}
}
