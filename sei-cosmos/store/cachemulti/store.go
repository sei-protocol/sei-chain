package cachemulti

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	dbm "github.com/tendermint/tm-db"

	gigacachekv "github.com/sei-protocol/sei-chain/giga/deps/store"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/tracekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

// freezable is implemented by cache stores that can be marked as superseded by a
// newer layer so that deeper layers may skip them for reads while they are empty,
// and reverted back to writable when a layer is re-exposed.
type freezable interface {
	Freeze()
	Unfreeze()
}

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

	// frozen marks this layer as superseded by a newer cache layer. Set opt-in via
	// Freeze(); shared (pointer) across value copies of the same layer so that
	// stores materialized lazily after freezing are also frozen.
	frozen *atomic.Bool

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
		frozen:          &atomic.Bool{},
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
		frozen := cms.frozen != nil && cms.frozen.Load()
		for k := range cms.parents {
			// Inline the creation here — we already hold the write lock.
			parent := cms.parents[k]
			var cw = parent
			if cms.TracingEnabled() {
				cw = tracekv.NewStore(parent.(types.KVStore), cms.traceWriter, cms.traceContext)
			}
			s := cachekv.NewStore(cw.(types.KVStore), k, types.DefaultCacheSizeLimit)
			if frozen {
				s.Freeze()
			}
			cms.stores[k] = s
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
	if cms.frozen != nil && cms.frozen.Load() {
		s.Freeze()
	}
	cms.stores[key] = s
	delete(cms.parents, key)
	return s
}

// Freeze marks the layer (and every materialized cache store within it) as
// superseded by a newer cache layer, so deeper layers may skip it for reads while
// it is empty. Stores materialized after Freeze inherit the frozen state via the
// shared flag. Freeze is opt-in and idempotent; layers that are never frozen keep
// the prior behavior exactly.
func (cms Store) Freeze() {
	if cms.frozen != nil {
		cms.frozen.Store(true)
	}
	if f, ok := cms.db.(freezable); ok {
		f.Freeze()
	}
	cms.mu.RLock()
	for _, s := range cms.stores {
		if f, ok := s.(freezable); ok {
			f.Freeze()
		}
	}
	cms.mu.RUnlock()
	for _, s := range cms.gigaStores {
		if f, ok := s.(freezable); ok {
			f.Freeze()
		}
	}
}

// Unfreeze reverts Freeze for the layer and every materialized cache store within
// it. It is called when the layer is re-exposed as the newest (writable) layer,
// e.g. by RevertToSnapshot, so that it is no longer skipped for reads. Stores
// materialized after Unfreeze see the cleared flag and are not frozen.
func (cms Store) Unfreeze() {
	if cms.frozen != nil {
		cms.frozen.Store(false)
	}
	if f, ok := cms.db.(freezable); ok {
		f.Unfreeze()
	}
	cms.mu.RLock()
	for _, s := range cms.stores {
		if f, ok := s.(freezable); ok {
			f.Unfreeze()
		}
	}
	cms.mu.RUnlock()
	for _, s := range cms.gigaStores {
		if f, ok := s.(freezable); ok {
			f.Unfreeze()
		}
	}
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
