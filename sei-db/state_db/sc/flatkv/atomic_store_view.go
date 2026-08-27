package flatkv

import (
	"fmt"
	"sync"
)

// atomicStoreView holds the store view of the most recently committed block and hands it out to
// readers on any thread.
//
// The execution thread advances it with set() as it commits, and it is the only thread that may. get()
// may be called from any thread, and promises a committed block that stays readable for as long as
// the caller holds the reservation get() took. It does not promise the latest block, and places no
// bound on how far behind the block it returns may be.
//
// The height only ever moves forward. Installing an earlier block means building a new
// atomicStoreView rather than reusing this one.
//
// It owns exactly one reservation on the view it holds, from construction until Close().
type atomicStoreView struct {
	// Guards currentView.
	mu sync.RWMutex

	// The most recently installed view. Nil once closed.
	currentView *storeView
}

// newAtomicStoreView() installs initialView. A non-nil initial view is required to simplify
// downstream logic (so it doesn't have to check for nil views).
func newAtomicStoreView(initialView *storeView) (*atomicStoreView, error) {
	if initialView == nil {
		return nil, fmt.Errorf("initial view is nil")
	}
	if err := initialView.reserve(); err != nil {
		return nil, fmt.Errorf("reserve initial view: %w", err)
	}
	return &atomicStoreView{currentView: initialView}, nil
}

// get() returns the installed view with a reservation the caller owns and must release exactly once.
func (asv *atomicStoreView) get() (*storeView, error) {
	asv.mu.RLock()
	defer asv.mu.RUnlock()

	if asv.currentView == nil {
		return nil, fmt.Errorf("atomic store view is closed")
	}
	if err := asv.currentView.reserve(); err != nil {
		return nil, fmt.Errorf("reserve view at height %d: %w", asv.currentView.blockHeight, err)
	}
	return asv.currentView, nil
}

// set() installs newView, which must describe a later block than the view already installed. The
// caller keeps its own reservation on newView and remains responsible for releasing it.
func (asv *atomicStoreView) set(newView *storeView) error {
	if newView == nil {
		return fmt.Errorf("new view is nil")
	}

	asv.mu.Lock()
	defer asv.mu.Unlock()

	if asv.currentView == nil {
		return fmt.Errorf("atomic store view is closed")
	}
	if newView.blockHeight <= asv.currentView.blockHeight {
		return fmt.Errorf("view height must advance: installed height %d, got %d",
			asv.currentView.blockHeight, newView.blockHeight)
	}

	// Reserved before the installed view is handed back, so a failure here leaves that view installed
	// and still readable rather than stranding readers on a view at zero reservations.
	if err := newView.reserve(); err != nil {
		return fmt.Errorf("reserve view at height %d: %w", newView.blockHeight, err)
	}

	previous := asv.currentView
	asv.currentView = newView
	if err := previous.release(); err != nil {
		return fmt.Errorf("release view at height %d: %w", previous.blockHeight, err)
	}
	return nil
}

// Close() hands back the reservation on the installed view and retires this atomicStoreView. get() and
// set() both fail afterwards. Idempotent.
func (asv *atomicStoreView) Close() error {
	asv.mu.Lock()
	defer asv.mu.Unlock()

	if asv.currentView == nil {
		return nil
	}

	previous := asv.currentView
	asv.currentView = nil
	if err := previous.release(); err != nil {
		return fmt.Errorf("release view at height %d: %w", previous.blockHeight, err)
	}
	return nil
}
