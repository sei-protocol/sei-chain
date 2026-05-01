package migration

import (
	"context"
	"sync"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"
)

var _ Router = (*threadSafeRouter)(nil)

// threadSafeRouter wraps an inner [Router] and serialises mutating operations
// against read operations using an RWMutex.
//
// Concurrency model:
//   - ApplyChangeSets holds the write lock, so it never overlaps with any
//     other Read / Iterator / GetProof / ApplyChangeSets call.
//   - Read, Iterator, and GetProof each hold the read lock, so they may run
//     concurrently with one another but never alongside an in-flight
//     ApplyChangeSets.
//
// Iterator caveat: Iterator() only holds the read lock while the call that
// constructs the iterator is on the stack. Iteration itself happens after
// the lock is released, so callers must not interleave ApplyChangeSets with
// use of an outstanding iterator obtained from the same router. That
// contract is by convention; this wrapper cannot enforce it because the
// iterator outlives the call that produced it.
type threadSafeRouter struct {
	mu    sync.RWMutex
	inner Router
}

// NewThreadSafeRouter wraps router so that ApplyChangeSets is mutually
// exclusive with Read / Iterator / GetProof, while the read-side methods
// may run concurrently with one another. See [threadSafeRouter] for the
// full concurrency contract, including the iterator caveat.
func NewThreadSafeRouter(router Router) Router {
	return &threadSafeRouter{inner: router}
}

func (r *threadSafeRouter) Read(store string, key []byte) ([]byte, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.Read(store, key)
}

func (r *threadSafeRouter) ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inner.ApplyChangeSets(ctx, changesets)
}

func (r *threadSafeRouter) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.Iterator(store, start, end, ascending)
}

func (r *threadSafeRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.GetProof(store, key)
}
