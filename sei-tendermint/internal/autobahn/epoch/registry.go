package epoch

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Registry holds the retained epoch window. Execution stores each window after the block that
// derives it, and consensus waits on the window it needs.
type Registry struct {
	window utils.AtomicSend[[]*types.Epoch]
}

// NewRegistry returns a registry holding epochs, oldest first. It returns an error when epochs is
// empty, or when the epochs are not consecutive, contiguous, and derived from one another.
func NewRegistry(epochs []*types.Epoch) (*Registry, error) {
	if err := checkWindow(epochs); err != nil {
		return nil, err
	}
	return &Registry{window: utils.NewAtomicSend(epochs)}, nil
}

// Epochs returns the stored window, oldest first, without waiting.
func (r *Registry) Epochs() []*types.Epoch {
	return r.window.Load()
}

// Wait blocks until ready accepts the stored window, and returns that window. ready returns false
// to keep waiting, and an error when no later window can satisfy the caller.
func (r *Registry) Wait(ctx context.Context, ready func([]*types.Epoch) (bool, error)) ([]*types.Epoch, error) {
	var readyErr error
	window, err := r.window.Subscribe().Wait(ctx, func(w []*types.Epoch) bool {
		ok, err := ready(w)
		if err != nil {
			readyErr = err
			return true
		}
		return ok
	})
	if readyErr != nil {
		return nil, readyErr
	}
	if err != nil {
		return nil, err
	}
	return window, nil
}

// Store publishes window as the retained epochs. It panics when window is empty, when its newest
// epoch has a lower index than the stored one, or when its epochs are not consecutive, contiguous,
// and derived from one another.
func (r *Registry) Store(window []*types.Epoch) {
	if err := checkWindow(window); err != nil {
		panic(err)
	}
	current := r.window.Load()
	if from, to := current[len(current)-1].EpochIndex(), window[len(window)-1].EpochIndex(); to < from {
		panic(fmt.Sprintf("epoch registry: stored window moves back from epoch %d to %d", from, to))
	}
	r.window.Store(window)
}
