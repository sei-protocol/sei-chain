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
	cms.mu.RLock()

	// Skip force-materialization of lazy parents. Instead, merge both
	// already-materialized stores and lazy storeParents into the child's
	// storeParents. This is safe because:
	//
	// - Materialized stores (in cms.stores): child wraps them, so
	//   child.Write() propagates through the parent's cache layer.
	// - Lazy stores (in cms.storeParents): the parent hasn't created a
	//   cachekv for them yet, so there's no stale cache. The child wraps
	//   the raw parent directly; child.Write() writes to the raw parent,
	//   and the parent's eventual cachekv (created on first access) will
	//   read through and see the child's writes.
	//
	// This eliminates ~13 GB of allocations per 30s from creating cachekv
	// stores that are either immediately replaced (OCC SetKVStores) or
	// never accessed (nested EVM Snapshots touching only 3-5 of ~20 stores).
	storeParents := make(map[types.StoreKey]types.CacheWrapper, len(cms.stores)+len(cms.storeParents))
	for k, v := range cms.stores {
		storeParents[k] = v
	}
	for k, v := range cms.storeParents {
		if _, exists := storeParents[k]; !exists {
			storeParents[k] = v
		}
	}

	// Merge giga stores: prefer materialized, fall back to lazy parents,
	// then fall back to regular stores/storeParents.
	gigaStoreParents := make(map[types.StoreKey]types.KVStore, len(cms.gigaKeys))
	for _, key := range cms.gigaKeys {
		if gigaStore, ok := cms.gigaStores[key]; ok {
			gigaStoreParents[key] = gigaStore
		} else if gigaParent, ok := cms.gigaStoreParents[key]; ok {
			gigaStoreParents[key] = gigaParent
		} else if store, ok := cms.stores[key]; ok {
			gigaStoreParents[key] = store.(types.KVStore)
		} else if parent, ok := cms.storeParents[key]; ok {
			gigaStoreParents[key] = parent.(types.KVStore)
		}
	}

	cms.mu.RUnlock()

	return Store{
		mu:               &sync.RWMutex{},
		db:               nil,
		stores:           make(map[types.StoreKey]types.CacheWrap, len(storeParents)),
		storeParents:     storeParents,
		keys:             cms.keys,
		gigaStores:       make(map[types.StoreKey]types.KVStore, len(gigaStoreParents)),
		gigaStoreParents: gigaStoreParents,
		gigaKeys:         cms.gigaKeys,
		traceWriter:      cms.traceWriter,
		traceContext:     cms.traceContext,
	}
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
	if cms.db != nil {
		cms.db.Write()
	}
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

// CacheMultiStoreForOCC creates a child CMS optimized for OCC scheduler use.
// Instead of copying storeParents maps and then immediately overriding via
// SetKVStores/SetGigaKVStores, this directly builds the stores/gigaStores maps
// using the provided handler functions, eliminating intermediate allocations.
func (cms Store) CacheMultiStoreForOCC(
	kvHandler func(sk types.StoreKey, kvs types.KVStore) types.CacheWrap,
	gigaHandler func(sk types.StoreKey, kvs types.KVStore) types.KVStore,
) types.MultiStore {
	cms.mu.RLock()

	// Build stores map directly from overrides — no storeParents copy
	stores := make(map[types.StoreKey]types.CacheWrap, len(cms.stores)+len(cms.storeParents))
	for k, s := range cms.stores {
		stores[k] = kvHandler(k, s.(types.KVStore))
	}
	for k, parent := range cms.storeParents {
		if _, exists := stores[k]; !exists {
			stores[k] = kvHandler(k, parent.(types.KVStore))
		}
	}

	gigaStores := make(map[types.StoreKey]types.KVStore, len(cms.gigaKeys))
	for _, key := range cms.gigaKeys {
		var parent types.KVStore
		if gs, ok := cms.gigaStores[key]; ok {
			parent = gs
		} else if gp, ok := cms.gigaStoreParents[key]; ok {
			parent = gp
		} else if s, ok := cms.stores[key]; ok {
			parent = s.(types.KVStore)
		} else if p, ok := cms.storeParents[key]; ok {
			parent = p.(types.KVStore)
		}
		if parent != nil {
			gigaStores[key] = gigaHandler(key, parent)
		}
	}

	cms.mu.RUnlock()

	return Store{
		mu:           &sync.RWMutex{},
		stores:       stores,
		gigaStores:   gigaStores,
		keys:         cms.keys,
		gigaKeys:     cms.gigaKeys,
		traceWriter:  cms.traceWriter,
		traceContext: cms.traceContext,
	}
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

// Release returns all pooled child stores back to their sync.Pools.
func (cms Store) Release() {
	cms.mu.Lock()
	for _, s := range cms.stores {
		if ckv, ok := s.(*cachekv.Store); ok {
			ckv.Release()
		}
	}
	if cms.db != nil {
		if ckv, ok := cms.db.(*cachekv.Store); ok {
			ckv.Release()
		}
	}
	for _, s := range cms.gigaStores {
		if gkv, ok := s.(*gigacachekv.Store); ok {
			gkv.Release()
		}
	}
	cms.mu.Unlock()
}

func (cms Store) GetEarliestVersion() int64 {
	panic("not implemented")
}
