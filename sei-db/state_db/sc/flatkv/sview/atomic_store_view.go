package sview

import (
	"fmt"
	"sync"
)

// AtomicStoreView holds one StoreView and hands it out to readers on any thread. It owns exactly one
// reservation on the view it holds, from construction until Close().
//
// All methods are safe to call concurrently. Set() rejects a view that does not advance the installed
// height, so that height strictly increases.
//
// The view Get() returns stays readable for as long as its caller holds the reservation Get() took,
// and is unaffected by views installed afterwards.
type AtomicStoreView struct {
	// Guards currentView.
	mu sync.RWMutex

	// The most recently installed view. Nil once closed.
	currentView *StoreView
}

// NewAtomicStoreView() installs initialView, which must be non-nil.
func NewAtomicStoreView(initialView *StoreView) (*AtomicStoreView, error) {
	if initialView == nil {
		return nil, fmt.Errorf("initial view is nil")
	}
	if err := initialView.Reserve(); err != nil {
		return nil, fmt.Errorf("reserve initial view: %w", err)
	}
	return &AtomicStoreView{currentView: initialView}, nil
}

// Get() returns the installed view with a reservation the caller owns and must release exactly once.
//
// Safe to call on a nil receiver, which reports an error rather than panicking.
func (asv *AtomicStoreView) Get() (*StoreView, error) {
	if asv == nil {
		return nil, fmt.Errorf("no sealed block: the store is not open")
	}

	asv.mu.RLock()
	defer asv.mu.RUnlock()

	if asv.currentView == nil {
		return nil, fmt.Errorf("atomic store view is closed")
	}
	if err := asv.currentView.Reserve(); err != nil {
		return nil, fmt.Errorf("reserve view at height %d: %w", asv.currentView.blockHeight, err)
	}
	return asv.currentView, nil
}

// Set() installs newView, which must describe a later block than the view already installed. The
// caller keeps its own reservation on newView and remains responsible for releasing it.
func (asv *AtomicStoreView) Set(newView *StoreView) error {
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

	// Reserved before the installed view is released, so a failure here leaves that view installed
	// and still readable rather than stranding readers on a view at zero reservations.
	if err := newView.Reserve(); err != nil {
		return fmt.Errorf("reserve view at height %d: %w", newView.blockHeight, err)
	}

	previous := asv.currentView
	asv.currentView = newView
	if err := previous.Release(); err != nil {
		return fmt.Errorf("release view at height %d: %w", previous.blockHeight, err)
	}
	return nil
}

// Close() releases the reservation on the installed view and retires this AtomicStoreView. Get() and
// Set() both fail afterwards. Idempotent.
func (asv *AtomicStoreView) Close() error {
	asv.mu.Lock()
	defer asv.mu.Unlock()

	if asv.currentView == nil {
		return nil
	}

	previous := asv.currentView
	asv.currentView = nil
	if err := previous.Release(); err != nil {
		return fmt.Errorf("release view at height %d: %w", previous.blockHeight, err)
	}
	return nil
}
