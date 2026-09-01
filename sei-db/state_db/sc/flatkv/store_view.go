package flatkv

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
)

// storeView is a read only view of all of FlatKV's stores at a single block height.
//
// It holds no reservation of its own. Reading through it requires a reservation, taken with reserve()
// or handed over by whoever took one already.
type storeView struct {
	// A read only view of the account store.
	accountStoreView view.View

	// A read only view of the code store.
	codeStoreView view.View

	// A read only view of the storage store.
	storageStoreView view.View

	// A read only view of the misc store.
	miscStoreView view.View

	// Every store's view, for the operations that treat them uniformly. In no particular order; a
	// caller that wants a specific store reads the field for it.
	viewSlice []view.View

	// The block height this view is targeted on.
	blockHeight int64
}

// newStoreView() describes the state of every store at blockHeight.
func newStoreView(
	blockHeight int64,
	accountStoreView view.View,
	codeStoreView view.View,
	storageStoreView view.View,
	miscStoreView view.View,
) (*storeView, error) {
	if accountStoreView == nil {
		return nil, fmt.Errorf("account view is nil")
	}
	if codeStoreView == nil {
		return nil, fmt.Errorf("code view is nil")
	}
	if storageStoreView == nil {
		return nil, fmt.Errorf("storage view is nil")
	}
	if miscStoreView == nil {
		return nil, fmt.Errorf("misc view is nil")
	}

	return &storeView{
		accountStoreView: accountStoreView,
		codeStoreView:    codeStoreView,
		storageStoreView: storageStoreView,
		miscStoreView:    miscStoreView,
		viewSlice: []view.View{
			accountStoreView, codeStoreView, storageStoreView, miscStoreView,
		},
		blockHeight: blockHeight,
	}, nil
}

// reserve() takes one reservation on every store's view. A failure stops there, leaving what it
// already took held: a view manager error is unrecoverable, so the node is going down anyway.
func (sv *storeView) reserve() error {
	for _, dbView := range sv.viewSlice {
		if err := dbView.Reserve(); err != nil {
			return fmt.Errorf("reserve %s view at height %d: %w", dbView.Name(), sv.blockHeight, err)
		}
	}
	return nil
}

// release() hands back one reservation on every store's view. A failure stops there, for the same
// reason reserve() does.
func (sv *storeView) release() error {
	for _, dbView := range sv.viewSlice {
		if err := dbView.Release(); err != nil {
			return fmt.Errorf("release %s view at height %d: %w", dbView.Name(), sv.blockHeight, err)
		}
	}
	return nil
}

// awaitFlush() blocks until every store has written this view's block to disk. The caller must hold a
// reservation across the call.
func (sv *storeView) awaitFlush(ctx context.Context) error {
	for _, dbView := range sv.viewSlice {
		if err := dbView.AwaitFlush(ctx); err != nil {
			return fmt.Errorf("await flush of %s at height %d: %w", dbView.Name(), sv.blockHeight, err)
		}
	}
	return nil
}
