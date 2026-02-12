package cachemulti

import (
	"fmt"
	"io"
	"sync"

	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	gigacachekv "github.com/sei-protocol/sei-chain/giga/deps/store"
)

//----------------------------------------
// Store

// Store holds many branched stores.
// Implements MultiStore.
// NOTE: a Store (and MultiStores in general) should never expose the
// keys for the substores.
type Store struct {
	mu               *sync.RWMutex
	db               types.CacheKVStore
	stores           map[types.StoreKey]types.CacheWrap
	storeParents     map[types.StoreKey]types.CacheWrapper // lazy: not yet wrapped in cachekv
	keys             map[string]types.StoreKey
	gigaStores       map[types.StoreKey]types.KVStore
	gigaStoreParents map[types.StoreKey]types.KVStore // lazy: not yet wrapped in giga cachekv
	gigaKeys         []types.StoreKey

	traceWriter  io.Writer
	traceContext types.TraceContext

	closers []io.Closer
}

var _ types.CacheMultiStore = Store{}

// NewFromKVStore creates a new Store object from a mapping of store keys to
// CacheWrapper objects and a KVStore as the database. Each CacheWrapper store
// is a branched store. Store creation is deferred until access time.
func NewFromKVStore(
	store types.KVStore, stores map[types.StoreKey]types.CacheWrapper,
	gigaStores map[types.StoreKey]types.KVStore,
	keys map[string]types.StoreKey, gigaKeys []types.StoreKey, traceWriter io.Writer, traceContext types.TraceContext,
) Store {
	cms := newStoreWithoutGiga(store, stores, keys, gigaKeys, traceWriter, traceContext)

	// Defer giga store creation: store parents, create cachekv wrappers on demand
	cms.gigaStores = make(map[types.StoreKey]types.KVStore, len(gigaKeys))
	cms.gigaStoreParents = make(map[types.StoreKey]types.KVStore, len(gigaKeys))
	for _, key := range gigaKeys {
		if gigaStore, ok := gigaStores[key]; ok {
			cms.gigaStoreParents[key] = gigaStore
		} else if parent, ok := cms.storeParents[key]; ok {
			cms.gigaStoreParents[key] = parent.(types.KVStore)
		}
	}

	return cms
}

func newStoreWithoutGiga(store types.KVStore, stores map[types.StoreKey]types.CacheWrapper, keys map[string]types.StoreKey, gigaKeys []types.StoreKey, traceWriter io.Writer, traceContext types.TraceContext) Store {
	cms := Store{
		mu:           &sync.RWMutex{},
		db:           cachekv.NewStore(store, nil, types.DefaultCacheSizeLimit),
		stores:       make(map[types.StoreKey]types.CacheWrap, len(stores)),
		storeParents: make(map[types.StoreKey]types.CacheWrapper, len(stores)),
		keys:         keys,
		gigaKeys:     gigaKeys,
		traceWriter:  traceWriter,
		traceContext: traceContext,
		closers:      []io.Closer{},
	}

	// Defer store creation: store parents, create cachekv wrappers on demand
	for key, store := range stores {
		cms.storeParents[key] = store
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
	cms.mu.Lock()

	// Materialize all lazy parents on the parent CMS first.
	// This ensures the child CMS wraps cachekv stores (not raw stores),
	// so child.Write() writes to the parent's cachekv — not directly to the
	// underlying commit store, which would bypass the parent's caching layer.
	for k := range cms.storeParents {
		cms.ensureStoreLocked(k)
	}
	for k := range cms.gigaStoreParents {
		cms.ensureGigaStoreLocked(k)
	}

	stores := make(map[types.StoreKey]types.CacheWrapper, len(cms.stores))
	for k, v := range cms.stores {
		stores[k] = v
	}

	gigaStores := make(map[types.StoreKey]types.KVStore, len(cms.gigaStores))
	for k, v := range cms.gigaStores {
		gigaStores[k] = v
	}

	cms.mu.Unlock()

	return NewFromKVStore(cms.db, stores, gigaStores, cms.keys, cms.gigaKeys, cms.traceWriter, cms.traceContext)
}

// ensureStore lazily creates a cachekv wrapper for a store key on first access.
// Thread-safe: uses double-checked locking so the common path (already materialized)
// only takes an RLock.
func (cms Store) ensureStore(key types.StoreKey) types.CacheWrap {
	if cms.mu == nil {
		// No lazy initialization possible (zero-value or eager store).
		s := cms.stores[key]
		return s
	}
	cms.mu.RLock()
	if s, ok := cms.stores[key]; ok {
		cms.mu.RUnlock()
		return s
	}
	cms.mu.RUnlock()

	cms.mu.Lock()
	defer cms.mu.Unlock()
	return cms.ensureStoreLocked(key)
}

// ensureStoreLocked materializes a store. Caller must hold cms.mu write lock.
func (cms Store) ensureStoreLocked(key types.StoreKey) types.CacheWrap {
	if s, ok := cms.stores[key]; ok {
		return s
	}
	parent, ok := cms.storeParents[key]
	if !ok {
		return nil
	}
	var kvParent types.KVStore = parent.(types.KVStore)
	if cms.TracingEnabled() {
		kvParent = tracekv.NewStore(kvParent, cms.traceWriter, cms.traceContext)
	}
	s := cachekv.NewStore(kvParent, key, types.DefaultCacheSizeLimit)
	cms.stores[key] = s
	delete(cms.storeParents, key)
	return s
}

// ensureGigaStore lazily creates a giga cachekv wrapper on first access.
// Thread-safe: uses double-checked locking.
func (cms Store) ensureGigaStore(key types.StoreKey) types.KVStore {
	if cms.mu == nil {
		s := cms.gigaStores[key]
		return s
	}
	cms.mu.RLock()
	if s, ok := cms.gigaStores[key]; ok {
		cms.mu.RUnlock()
		return s
	}
	cms.mu.RUnlock()

	cms.mu.Lock()
	defer cms.mu.Unlock()
	return cms.ensureGigaStoreLocked(key)
}

// ensureGigaStoreLocked materializes a giga store. Caller must hold cms.mu write lock.
func (cms Store) ensureGigaStoreLocked(key types.StoreKey) types.KVStore {
	if s, ok := cms.gigaStores[key]; ok {
		return s
	}
	parent, ok := cms.gigaStoreParents[key]
	if !ok {
		return nil
	}
	s := gigacachekv.NewStore(parent, key, types.DefaultCacheSizeLimit)
	cms.gigaStores[key] = s
	delete(cms.gigaStoreParents, key)
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
// Only materialized (accessed) stores need writing — lazy stores have no dirty data.
func (cms Store) Write() {
	cms.mu.RLock()
	cms.db.Write()
	for _, store := range cms.stores {
		store.Write()
	}
	cms.mu.RUnlock()
}

func (cms Store) WriteGiga() {
	cms.mu.RLock()
	for _, store := range cms.gigaStores {
		store.(types.CacheKVStore).Write()
	}
	cms.mu.RUnlock()
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
	s := cms.ensureStore(key)
	if key == nil || s == nil {
		panic(fmt.Sprintf("kv store with key %v has not been registered in stores", key))
	}
	return s.(types.Store)
}

// GetKVStore returns an underlying KVStore by key.
func (cms Store) GetKVStore(key types.StoreKey) types.KVStore {
	store := cms.ensureStore(key)
	if key == nil || store == nil {
		panic(fmt.Sprintf("kv store with key %v has not been registered in stores", key))
	}
	return store.(types.KVStore)
}

func (cms Store) GetGigaKVStore(key types.StoreKey) types.KVStore {
	store := cms.ensureGigaStore(key)
	if key == nil || store == nil {
		panic(fmt.Sprintf("giga kv store with key %v has not been registered in stores", key))
	}
	return store
}

func (cms Store) IsStoreGiga(key types.StoreKey) bool {
	cms.mu.RLock()
	defer cms.mu.RUnlock()
	if _, ok := cms.gigaStores[key]; ok {
		return true
	}
	_, ok := cms.gigaStoreParents[key]
	return ok
}

func (cms Store) GetWorkingHash() ([]byte, error) {
	panic("should never attempt to get working hash from cache multi store")
}

// StoreKeys returns a list of all store keys
func (cms Store) StoreKeys() []types.StoreKey {
	keys := make([]types.StoreKey, 0, len(cms.stores))
	for _, key := range cms.keys {
		keys = append(keys, key)
	}
	return keys
}

// SetKVStores sets the underlying KVStores via a handler for each key
func (cms Store) SetKVStores(handler func(sk types.StoreKey, s types.KVStore) types.CacheWrap) types.MultiStore {
	cms.mu.Lock()
	defer cms.mu.Unlock()
	// Process already-materialized stores
	for k, s := range cms.stores {
		cms.stores[k] = handler(k, s.(types.KVStore))
	}
	// Process lazy parents (pass the parent KVStore directly to handler)
	for k, parent := range cms.storeParents {
		if _, exists := cms.stores[k]; !exists {
			cms.stores[k] = handler(k, parent.(types.KVStore))
		}
	}
	// All parents have been processed
	for k := range cms.storeParents {
		delete(cms.storeParents, k)
	}
	return cms
}

func (cms Store) SetGigaKVStores(handler func(sk types.StoreKey, s types.KVStore) types.KVStore) types.MultiStore {
	cms.mu.Lock()
	defer cms.mu.Unlock()
	// Process already-materialized giga stores
	for k, s := range cms.gigaStores {
		cms.gigaStores[k] = handler(k, s)
	}
	// Process lazy giga parents
	for k, parent := range cms.gigaStoreParents {
		if _, exists := cms.gigaStores[k]; !exists {
			cms.gigaStores[k] = handler(k, parent)
		}
	}
	// All parents have been processed
	for k := range cms.gigaStoreParents {
		delete(cms.gigaStoreParents, k)
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
		closer.Close()
	}
}

func (cms Store) GetEarliestVersion() int64 {
	panic("not implemented")
}
