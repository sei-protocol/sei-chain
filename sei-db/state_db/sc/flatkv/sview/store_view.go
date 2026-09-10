package sview

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
)

// StoreView is a read only view of all of FlatKV's stores at a single block height.
//
// It holds no reservation of its own. Reading through it requires a reservation, taken with Reserve()
// or handed over by whoever took one already.
type StoreView struct {
	// A read only view of the account store.
	accountStoreView view.View

	// A read only view of the code store.
	codeStoreView view.View

	// A read only view of the storage store.
	storageStoreView view.View

	// A read only view of the misc store.
	miscStoreView view.View

	// Every store's view, for the operations that treat them uniformly. In no particular order; a
	// caller that wants a specific store reads the accessor for it.
	viewSlice []view.View

	// The block height this view is targeted on.
	blockHeight int64
}

// NewStoreView() describes the state of every store at blockHeight.
func NewStoreView(
	blockHeight int64,
	accountStoreView view.View,
	codeStoreView view.View,
	storageStoreView view.View,
	miscStoreView view.View,
) (*StoreView, error) {
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

	return &StoreView{
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

// BlockHeight() returns the block height this view is targeted on.
func (sv *StoreView) BlockHeight() int64 {
	return sv.blockHeight
}

// AccountView() returns the account store's view.
func (sv *StoreView) AccountView() view.View {
	return sv.accountStoreView
}

// CodeView() returns the code store's view.
func (sv *StoreView) CodeView() view.View {
	return sv.codeStoreView
}

// StorageView() returns the storage store's view.
func (sv *StoreView) StorageView() view.View {
	return sv.storageStoreView
}

// MiscView() returns the misc store's view.
func (sv *StoreView) MiscView() view.View {
	return sv.miscStoreView
}

// Views() returns every store's view, for the operations that treat them uniformly. The order is
// unspecified, and the returned slice must not be modified.
func (sv *StoreView) Views() []view.View {
	return sv.viewSlice
}

// Reserve() takes one reservation on every store's view. A failure stops there, leaving what it
// already took held: a view manager error is unrecoverable, so the node is going down anyway.
func (sv *StoreView) Reserve() error {
	for _, dbView := range sv.viewSlice {
		if err := dbView.Reserve(); err != nil {
			return fmt.Errorf("reserve %s view at height %d: %w", dbView.Name(), sv.blockHeight, err)
		}
	}
	return nil
}

// Releases one reservation on every store's view. A failure stops there, for the same reason
// Reserve() does.
func (sv *StoreView) Release() error {
	for _, dbView := range sv.viewSlice {
		if err := dbView.Release(); err != nil {
			return fmt.Errorf("release %s view at height %d: %w", dbView.Name(), sv.blockHeight, err)
		}
	}
	return nil
}

// AwaitFlush() blocks until every store has written this view's block to disk. The caller must hold a
// reservation across the call.
func (sv *StoreView) AwaitFlush(ctx context.Context) error {
	for _, dbView := range sv.viewSlice {
		if err := dbView.AwaitFlush(ctx); err != nil {
			return fmt.Errorf("await flush of %s at height %d: %w", dbView.Name(), sv.blockHeight, err)
		}
	}
	return nil
}
