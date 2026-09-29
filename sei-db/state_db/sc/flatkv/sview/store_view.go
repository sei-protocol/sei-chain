package sview

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// StoreView is a read only view of all of FlatKV's stores at a single block height.
//
// It holds no reservation of its own. Reading through it requires a reservation, taken with Reserve()
// or handed over by whoever took one already.
type StoreView struct {
	// A read only view of the account store.
	accountStoreView view.View[vtype.AccountData]

	// A read only view of the code store.
	codeStoreView view.View[vtype.CodeData]

	// A read only view of the storage store.
	storageStoreView view.View[vtype.StorageData]

	// A read only view of the misc store.
	miscStoreView view.View[vtype.MiscData]

	// The block height this view is targeted on.
	blockHeight int64
}

// NewStoreView() describes the state of every store at blockHeight.
func NewStoreView(
	blockHeight int64,
	accountStoreView view.View[vtype.AccountData],
	codeStoreView view.View[vtype.CodeData],
	storageStoreView view.View[vtype.StorageData],
	miscStoreView view.View[vtype.MiscData],
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
		blockHeight:      blockHeight,
	}, nil
}

// BlockHeight() returns the block height this view is targeted on.
func (sv *StoreView) BlockHeight() int64 {
	return sv.blockHeight
}

// AccountView() returns the account store's view.
func (sv *StoreView) AccountView() view.View[vtype.AccountData] {
	return sv.accountStoreView
}

// CodeView() returns the code store's view.
func (sv *StoreView) CodeView() view.View[vtype.CodeData] {
	return sv.codeStoreView
}

// StorageView() returns the storage store's view.
func (sv *StoreView) StorageView() view.View[vtype.StorageData] {
	return sv.storageStoreView
}

// MiscView() returns the misc store's view.
func (sv *StoreView) MiscView() view.View[vtype.MiscData] {
	return sv.miscStoreView
}

// Reserve() takes one reservation on every store's view. A failure stops there and abandons every view,
// leaving what it already took held: a view manager error is unrecoverable, so the node is going down
// anyway.
func (sv *StoreView) Reserve() error {
	if err := sv.accountStoreView.Reserve(); err != nil {
		return sv.abandonAfter("reserve", sv.accountStoreView.Name(), err)
	}
	if err := sv.codeStoreView.Reserve(); err != nil {
		return sv.abandonAfter("reserve", sv.codeStoreView.Name(), err)
	}
	if err := sv.storageStoreView.Reserve(); err != nil {
		return sv.abandonAfter("reserve", sv.storageStoreView.Name(), err)
	}
	if err := sv.miscStoreView.Reserve(); err != nil {
		return sv.abandonAfter("reserve", sv.miscStoreView.Name(), err)
	}
	return nil
}

// Releases one reservation on every store's view. A failure stops there and abandons every view, for
// the same reason Reserve() does.
func (sv *StoreView) Release() error {
	if err := sv.accountStoreView.Release(); err != nil {
		return sv.abandonAfter("release", sv.accountStoreView.Name(), err)
	}
	if err := sv.codeStoreView.Release(); err != nil {
		return sv.abandonAfter("release", sv.codeStoreView.Name(), err)
	}
	if err := sv.storageStoreView.Release(); err != nil {
		return sv.abandonAfter("release", sv.storageStoreView.Name(), err)
	}
	if err := sv.miscStoreView.Release(); err != nil {
		return sv.abandonAfter("release", sv.miscStoreView.Name(), err)
	}
	return nil
}

// abandonAfter() abandons every store's view following a failed operation on the named one, and returns
// that failure.
func (sv *StoreView) abandonAfter(operation string, name string, err error) error {
	sv.Abandon()
	return fmt.Errorf("%s %s view at height %d: %w", operation, name, sv.blockHeight, err)
}

// Abandon() gives up on every store's view without releasing its reservations. It is for failure paths
// on which the node is going down. A no-op on a nil receiver.
func (sv *StoreView) Abandon() {
	if sv == nil {
		return
	}
	sv.accountStoreView.Abandon()
	sv.codeStoreView.Abandon()
	sv.storageStoreView.Abandon()
	sv.miscStoreView.Abandon()
}

// AwaitFlush() blocks until every store has written this view's block to disk. The caller must hold a
// reservation across the call.
func (sv *StoreView) AwaitFlush(ctx context.Context) error {
	if err := sv.accountStoreView.AwaitFlush(ctx); err != nil {
		return fmt.Errorf("await flush of %s at height %d: %w", sv.accountStoreView.Name(), sv.blockHeight, err)
	}
	if err := sv.codeStoreView.AwaitFlush(ctx); err != nil {
		return fmt.Errorf("await flush of %s at height %d: %w", sv.codeStoreView.Name(), sv.blockHeight, err)
	}
	if err := sv.storageStoreView.AwaitFlush(ctx); err != nil {
		return fmt.Errorf("await flush of %s at height %d: %w", sv.storageStoreView.Name(), sv.blockHeight, err)
	}
	if err := sv.miscStoreView.AwaitFlush(ctx); err != nil {
		return fmt.Errorf("await flush of %s at height %d: %w", sv.miscStoreView.Name(), sv.blockHeight, err)
	}
	return nil
}
