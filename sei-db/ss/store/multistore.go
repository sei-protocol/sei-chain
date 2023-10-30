package store

import (
	"io"
	"math"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/mem"
	"github.com/cosmos/cosmos-sdk/store/transient"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-db/sc/memiavl/store/cachemulti"
	sstypes "github.com/sei-protocol/sei-db/ss/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

var _ types.MultiStore = (*MultiStore)(nil)

// MultiStore wraps `StateStore` to implement `MultiStore` interface.
type MultiStore struct {
	stateStore sstypes.StateStore
	storeKeys  []types.StoreKey

	// transient or memory stores
	transientStores map[types.StoreKey]types.KVStore

	traceWriter       io.Writer
	traceContext      types.TraceContext
	traceContextMutex sync.Mutex
}

// NewMultiStore returns a new state store `MultiStore`.
func NewMultiStore(store sstypes.StateStore, storeKeys []types.StoreKey) *MultiStore {
	return &MultiStore{
		stateStore:      store,
		storeKeys:       storeKeys,
		transientStores: make(map[types.StoreKey]types.KVStore),
	}
}

func (s *MultiStore) GetStoreType() types.StoreType {
	return types.StoreTypeMulti
}

// cacheMultiStore branch out the multistore.
func (s *MultiStore) cacheMultiStore(version int64) types.CacheMultiStore {
	stores := make(map[types.StoreKey]types.CacheWrapper, len(s.transientStores)+len(s.storeKeys))
	for k, v := range s.transientStores {
		stores[k] = v
	}
	for _, k := range s.storeKeys {
		stores[k] = NewKVStore(s.stateStore, k, version)
	}
	return cachemulti.NewStore(nil, stores, nil, s.traceWriter, s.getTracingContext(), nil, nil)
}

func (s *MultiStore) CacheMultiStore() types.CacheMultiStore {
	return s.cacheMultiStore(math.MaxInt64)
}

func (s *MultiStore) CacheMultiStoreWithVersion(version int64) (types.CacheMultiStore, error) {
	return s.cacheMultiStore(version), nil
}

func (s *MultiStore) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return s.CacheMultiStore().CacheWrap(storeKey)
}

func (s *MultiStore) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return s.CacheMultiStore().CacheWrapWithTrace(storeKey, w, tc)
}

func (s *MultiStore) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return s.CacheMultiStore().CacheWrapWithListeners(storeKey, listeners)
}

func (s *MultiStore) GetStore(key types.StoreKey) types.Store {
	return s.GetKVStore(key)
}

func (s *MultiStore) GetKVStore(key types.StoreKey) types.KVStore {
	store, ok := s.transientStores[key]
	if ok {
		return store
	}
	return NewKVStore(s.stateStore, key, math.MaxInt64)
}

func (s *MultiStore) TracingEnabled() bool {
	return s.traceWriter != nil
}

func (s *MultiStore) SetTracer(w io.Writer) types.MultiStore {
	s.traceWriter = w
	return s
}

func (s *MultiStore) SetTracingContext(context types.TraceContext) types.MultiStore {
	s.traceContextMutex.Lock()
	defer s.traceContextMutex.Unlock()
	s.traceContext = context
	return s
}

func (s *MultiStore) getTracingContext() types.TraceContext {
	s.traceContextMutex.Lock()
	defer s.traceContextMutex.Unlock()
	if s.traceContext == nil {
		return nil
	}

	ctx := types.TraceContext{}
	for k, v := range s.traceContext {
		ctx[k] = v
	}

	return ctx
}

// MountTransientStores simulates the same behavior as sdk to support grpc query service.
func (s *MultiStore) MountTransientStores(keys map[string]*types.TransientStoreKey) {
	for _, key := range keys {
		s.transientStores[key] = transient.NewStore()
	}
}

// MountMemoryStores simulates the same behavior as sdk to support grpc query service.
func (s *MultiStore) MountMemoryStores(keys map[string]*types.MemoryStoreKey) {
	for _, key := range keys {
		s.transientStores[key] = mem.NewStore()
	}
}

func (s *MultiStore) ListeningEnabled(_ types.StoreKey) bool {
	return false
}

func (s *MultiStore) AddListeners(_ types.StoreKey, _ []types.WriteListener) {
	panic("not supported")
}

func (s *MultiStore) GetWorkingHash() ([]byte, error) {
	panic("not supported")
}

func (s *MultiStore) GetEvents() []abci.Event {
	panic("not supported")
}

func (s *MultiStore) ResetEvents() {
	panic("not supported")
}
