package baseapp

import (
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type state struct {
	ms  sdk.CacheMultiStore
	ctx sdk.Context
	mtx *sync.RWMutex
}

// CacheMultiStore calls and returns a CacheMultiStore on the state's underling
// CacheMultiStore.
func (st *state) CacheMultiStore() sdk.CacheMultiStore {
	st.mtx.RLock()
	defer st.mtx.RUnlock()
	return st.ms.CacheMultiStore()
}

func (st *state) MultiStore() sdk.CacheMultiStore {
	st.mtx.RLock()
	defer st.mtx.RUnlock()
	return st.ms
}

func (st *state) SetMultiStore(ms sdk.CacheMultiStore) *state {
	st.mtx.Lock()
	defer st.mtx.Unlock()
	st.ms = ms
	return st
}

// Context returns the Context of the state.
func (st *state) Context() sdk.Context {
	st.mtx.RLock()
	defer st.mtx.RUnlock()
	return st.ctx
}

func (st *state) SetContext(ctx sdk.Context) *state {
	st.mtx.Lock()
	defer st.mtx.Unlock()
	st.ctx = ctx
	return st
}
