package flatkv

import (
	"context"
	"fmt"
	"sync"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// hashListenerRegistry holds the listeners each block's hash is dispatched to, and the most recent
// hash they were given.
//
// The store owns it rather than the finalization manager that dispatches through it, so that
// registrations outlive a manager rebuilt underneath them — which is what rollback and restore do.
//
// Every method is safe to call from any goroutine.
type hashListenerRegistry struct {
	// mu guards both fields below as one. A registration that interleaved with a dispatch would
	// either miss a block or be told the wrong block to expect next.
	mu sync.Mutex

	// The listeners, in registration order.
	listeners []gigatypes.HashListener

	// The most recent hash handed to the listeners, which is the block a listener registering now
	// is told its first delivery follows. Nil until a block has been dispatched.
	lastDispatched *lthash.BlockHash
}

// newHashListenerRegistry returns an empty registry.
func newHashListenerRegistry() *hashListenerRegistry {
	return &hashListenerRegistry{}
}

// register adds a listener and reports the most recent hash dispatched before it was added, or
// current when nothing has been dispatched yet.
func (r *hashListenerRegistry) register(
	listener gigatypes.HashListener,
	// The height the store stands at, for a store that has dispatched nothing.
	current *lthash.BlockHash,
) lthash.BlockHash {
	r.mu.Lock()
	defer r.mu.Unlock()

	// A caller that only wants the hash passes nil rather than a callback it does not need.
	if listener != nil {
		r.listeners = append(r.listeners, listener)
	}
	if r.lastDispatched == nil {
		return *current
	}
	return *r.lastDispatched
}

// dispatch hands one block's hash to every registered listener, reporting the first refusal.
func (r *hashListenerRegistry) dispatch(ctx context.Context, hash *lthash.BlockHash) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, listener := range r.listeners {
		if err := listener(ctx, hash.BlockNumber, hash); err != nil {
			return fmt.Errorf("a hash listener refused block %d: %w", hash.BlockNumber, err)
		}
	}
	r.lastDispatched = hash
	return nil
}
