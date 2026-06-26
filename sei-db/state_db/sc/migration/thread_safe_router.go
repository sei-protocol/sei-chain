package migration

import (
	"fmt"
	"sync"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ Router = (*threadSafeRouter)(nil)

// threadSafeRouter wraps an inner [Router] and serialises mutating operations
// against read operations using an RWMutex.
//
// Concurrency model:
//   - ApplyChangeSets holds the write lock, so it never overlaps with any
//     other Read / GetProof / ApplyChangeSets call.
//   - Read and GetProof each hold the read lock, so they may run concurrently
//     with one another but never alongside an in-flight ApplyChangeSets.
//
// Iteration is not routed through the Router (the owning store stitches the
// backends together directly), so the wrapper has no Iterator method.
type threadSafeRouter struct {
	mu    sync.RWMutex
	inner Router
}

// NewThreadSafeRouter wraps router so that ApplyChangeSets is mutually
// exclusive with Read / GetProof, while the read-side methods may run
// concurrently with one another. See [threadSafeRouter] for the full
// concurrency contract.
//
// Returns an error if router is nil.
func NewThreadSafeRouter(router Router) (Router, error) {
	if router == nil {
		return nil, fmt.Errorf("router must not be nil")
	}
	return &threadSafeRouter{inner: router}, nil
}

func (r *threadSafeRouter) Read(store string, key []byte) ([]byte, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.Read(store, key)
}

func (r *threadSafeRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inner.ApplyChangeSets(changesets, firstBatchInBlock)
}

func (r *threadSafeRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.GetProof(store, key)
}

// SetMigrationBatchSize forwards to the inner router under the write lock,
// so the update is serialized against in-flight Read / GetProof /
// ApplyChangeSets calls.
func (r *threadSafeRouter) SetMigrationBatchSize(batchSize int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inner.SetMigrationBatchSize(batchSize)
}
