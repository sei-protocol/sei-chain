package cachemulti

import (
	"fmt"
	"io"
	"sync"

	dbm "github.com/tendermint/tm-db"

	gigacachekv "github.com/sei-protocol/sei-chain/giga/deps/store"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/tracekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

//----------------------------------------
// Store

// Store holds many branched stores.
// Implements MultiStore.
// NOTE: a Store (and MultiStores in general) should never expose the
// keys for the substores.
type Store struct {
	db      types.CacheKVStore
	stores  map[types.StoreKey]types.CacheWrap
	parents map[types.StoreKey]types.CacheWrapper
	keys    map[string]types.StoreKey

	gigaStores map[types.StoreKey]types.KVStore
	gigaKeys   []types.StoreKey

	traceWriter  io.Writer
	traceContext types.TraceContext

	mu              *sync.RWMutex // protects stores and parents during lazy creation
	materializeOnce *sync.Once

	closers []io.Closer
}

var _ types.CacheMultiStore = Store{}

// NewFromKVStore creates a new Store object from a mapping of store keys to
// CacheWrapper objects and a KVStore as the database. Each CacheWrapper store
// is a branched store.
func NewFromKVStore(
	store types.KVStore, stores map[types.StoreKey]types.CacheWrapper,
	gigaStores map[types.StoreKey]types.KVStore,
	keys map[string]types.StoreKey, gigaKeys []types.StoreKey, traceWriter io.Writer, traceContext types.TraceContext,
) Store {
	cms := newStoreWithoutGiga(store, stores, keys, gigaKeys, traceWriter, traceContext)

	cms.gigaStores = make(map[types.StoreKey]types.KVStore, len(gigaKeys))
	for _, key := range gigaKeys {
		if gigaStore, ok := gigaStores[key]; ok {
			// if key is in gigaStores, use it as the parent store
			cms.gigaStores[key] = gigacachekv.NewStore(gigaStore, key, types.DefaultCacheSizeLimit)
		} else {
			// if not, use regular store as the parent store
			parent := stores[key].(types.KVStore)
			cms.gigaStores[key] = gigacachekv.NewStore(parent, key, types.DefaultCacheSizeLimit)
		}
	}

	return cms
}

func newStoreWithoutGiga(store types.KVStore, stores map[types.StoreKey]types.CacheWrapper, keys map[string]types.StoreKey, gigaKeys []types.StoreKey, traceWriter io.Writer, traceContext types.TraceContext) Store {
	cms := Store{
		db:              cachekv.NewStore(store, nil, types.DefaultCacheSizeLimit),
		stores:          make(map[types.StoreKey]types.CacheWrap, len(stores)),
		parents:         make(map[types.StoreKey]types.CacheWrapper, len(stores)),
		keys:            keys,
		gigaKeys:        gigaKeys,
		traceWriter:     traceWriter,
		traceContext:    traceContext,
		mu:              &sync.RWMutex{},
		materializeOnce: &sync.Once{},
		closers:         []io.Closer{},
	}

	for key, store := range stores {
		cms.parents[key] = store
	}
	return cms
}

// NewStore creates a new Store object from a mapping of store keys to
// CacheWrapper objects. Each CacheWrapper store is a branched store.
func NewStore(
	db dbm.DB, stores map[types.StoreKey]types.CacheWrapper, keys map[string]types.StoreKey,
	gigaKeys []types.StoreKey, traceWriter io.Writer, traceContext types.TraceContext,
) Store {

	return newStoreWithoutGiga(dbadapter.Store{DB: db}, stores, keys, gigaKeys, traceWriter, traceContext)
}

func newCacheMultiStoreFromCMS(cms Store) Store {
	// Thread-safe materialization: the OCC scheduler calls CacheMultiStore()
	// concurrently from multiple goroutines on the same block CMS.
	// sync.Once ensures exactly one goroutine materializes, others wait.
	cms.materializeOnce.Do(func() {
		// Lock held for bulk materialization to avoid per-key lock overhead.
		cms.mu.Lock()
		for k := range cms.parents {
			// Inline the creation here — we already hold the write lock.
			parent := cms.parents[k]
			var cw = parent
			if cms.TracingEnabled() {
				cw = tracekv.NewStore(parent.(types.KVStore), cms.traceWriter, cms.traceContext)
			}
			cms.stores[k] = cachekv.NewStore(cw.(types.KVStore), k, types.DefaultCacheSizeLimit)
			delete(cms.parents, k)
		}
		cms.mu.Unlock()
	})

	// After Do returns, cms.parents is empty and cms.stores has all entries.
	cms.mu.RLock()
	stores := make(map[types.StoreKey]types.CacheWrapper, len(cms.stores))
	for k, v := range cms.stores {
		stores[k] = v
	}
	cms.mu.RUnlock()
	// cms.parents is now empty — all moved to cms.stores by getOrCreateStore
	gigaStores := make(map[types.StoreKey]types.KVStore, len(cms.gigaStores))
	for k, v := range cms.gigaStores {
		gigaStores[k] = v
	}

	return NewFromKVStore(cms.db, stores, gigaStores, cms.keys, cms.gigaKeys, cms.traceWriter, cms.traceContext)
}

// getOrCreateStore lazily creates a cachekv store from its parent on first access.
// Thread-safe: concurrent callers (e.g. slashing BeginBlocker goroutines) may
// call GetKVStore on the same CMS simultaneously.
func (cms Store) getOrCreateStore(key types.StoreKey) types.CacheWrap {
	// Fast path: store already materialized, read-only check.
	cms.mu.RLock()
	if s, ok := cms.stores[key]; ok {
		cms.mu.RUnlock()
		return s
	}
	cms.mu.RUnlock()

	// Slow path: acquire write lock and create.
	cms.mu.Lock()
	defer cms.mu.Unlock()

	// Double-check after acquiring write lock.
	if s, ok := cms.stores[key]; ok {
		return s
	}
	parent, ok := cms.parents[key]
	if !ok {
		return nil
	}
	var cw = parent
	if cms.TracingEnabled() {
		cw = tracekv.NewStore(parent.(types.KVStore), cms.traceWriter, cms.traceContext)
	}
	s := cachekv.NewStore(cw.(types.KVStore), key, types.DefaultCacheSizeLimit)
	cms.stores[key] = s
	delete(cms.parents, key)
	return s
}

// SetTracer sets the tracer for the MultiStore that the underlying
// stores will utilize to trace operations. A MultiStore is returned.
func (cms Store) SetTracer(w io.Writer) types.MultiStore {
	cms.traceWriter = w
	return cms
}

// SetTracingContext updates the tracing context for the MultiStore by merging
// the given context with the existing context by key. Any existing keys will
// be overwritten. It is implied that the caller should update the context when
// necessary between tracing operations. It returns a modified MultiStore.
func (cms Store) SetTracingContext(tc types.TraceContext) types.MultiStore {
	if cms.traceContext != nil {
		for k, v := range tc {
			cms.traceContext[k] = v
		}
	} else {
		cms.traceContext = tc
	}

	return cms
}

// TracingEnabled returns if tracing is enabled for the MultiStore.
func (cms Store) TracingEnabled() bool {
	return cms.traceWriter != nil
}

// GetStoreType returns the type of the store.
func (cms Store) GetStoreType() types.StoreType {
	return types.StoreTypeMulti
}

// Write calls Write on each underlying store.
func (cms Store) Write() {
	cms.db.Write()
	for _, store := range cms.stores {
		store.Write()
	}
}

func (cms Store) WriteGiga() {
	for _, store := range cms.gigaStores {
		store.(types.CacheKVStore).Write()
	}
}

// Implements CacheWrapper.
func (cms Store) CacheWrap(_ types.StoreKey) types.CacheWrap {
	return cms.CacheMultiStore().(types.CacheWrap)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (cms Store) CacheWrapWithTrace(storeKey types.StoreKey, _ io.Writer, _ types.TraceContext) types.CacheWrap {
	return cms.CacheWrap(storeKey)
}

// Implements MultiStore.
func (cms Store) CacheMultiStore() types.CacheMultiStore {
	return newCacheMultiStoreFromCMS(cms)
}

// CacheMultiStoreWithVersion implements the MultiStore interface. It will panic
// as an already cached multi-store cannot load previous versions.
//
// TODO: The store implementation can possibly be modified to support this as it
// seems safe to load previous versions (heights).
func (cms Store) CacheMultiStoreWithVersion(_ int64) (types.CacheMultiStore, error) {
	panic("cannot branch cached multi-store with a version")
}

// GetStore returns an underlying Store by key.
func (cms Store) GetStore(key types.StoreKey) types.Store {
	s := cms.getOrCreateStore(key)
	if key == nil || s == nil {
		panic(fmt.Sprintf("kv store with key %v has not been registered in stores", key))
	}
	return s.(types.Store)
}

// GetKVStore returns an underlying KVStore by key.
func (cms Store) GetKVStore(key types.StoreKey) types.KVStore {
	s := cms.getOrCreateStore(key)
	if key == nil || s == nil {
		panic(fmt.Sprintf("kv store with key %v has not been registered in stores", key))
	}
	return s.(types.KVStore)
}

func (cms Store) GetGigaKVStore(key types.StoreKey) types.KVStore {
	store := cms.gigaStores[key]
	if key == nil || store == nil {
		panic(fmt.Sprintf("giga kv store with key %v has not been registered in stores", key))
	}
	return store
}

func (cms Store) IsStoreGiga(key types.StoreKey) bool {
	_, ok := cms.gigaStores[key]
	return ok
}

func (cms Store) GetWorkingHash() ([]byte, error) {
	panic("should never attempt to get working hash from cache multi store")
}

// StoreKeys returns a list of all store keys
func (cms Store) StoreKeys() []types.StoreKey {
	keys := make([]types.StoreKey, 0, len(cms.keys))
	for _, key := range cms.keys {
		keys = append(keys, key)
	}
	return keys
}

// SetKVStores sets the underlying KVStores via a handler for each key
func (cms Store) SetKVStores(handler func(sk types.StoreKey, s types.KVStore) types.CacheWrap) types.MultiStore {
	// Force-create any lazy stores
	for k := range cms.parents {
		cms.getOrCreateStore(k)
	}
	for k, s := range cms.stores {
		cms.stores[k] = handler(k, s.(types.KVStore))
	}
	return cms
}

func (cms Store) SetGigaKVStores(handler func(sk types.StoreKey, s types.KVStore) types.KVStore) types.MultiStore {
	for k, s := range cms.gigaStores {
		cms.gigaStores[k] = handler(k, s)
	}
	return cms
}

func (cms Store) CacheMultiStoreForExport(_ int64) (types.CacheMultiStore, error) {
	panic("Not implemented")
}

func (cms *Store) AddCloser(closer io.Closer) {
	cms.closers = append(cms.closers, closer)
}

func (cms Store) Close() {
	for _, closer := range cms.closers {
		_ = closer.Close()
	}
}

func (cms Store) GetEarliestVersion() int64 {
	panic("not implemented")
}
