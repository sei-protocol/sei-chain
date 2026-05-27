package utils

import (
	"context"
	"iter"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// Mutex guards access to object of type T.
type Mutex[T any] struct {
	mu    sync.Mutex
	value T
}

// NewMutex creates a new Mutex with given object.
func NewMutex[T any](value T) (m Mutex[T]) {
	m.value = value
	// nolint:nakedret
	return
}

// Lock returns an iterator which locks the mutex and yields the guarded object.
// The mutex is unlocked when the iterator is done.
// If the mutex is nil, the iterator is a no-op.
func (m *Mutex[T]) Lock() iter.Seq[T] {
	return func(yield func(val T) bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		_ = yield(m.value)
	}
}

// Mutex guards access to object of type T.
type RWMutex[T any] struct {
	mu    sync.RWMutex
	value T
}

// NewMutex creates a new Mutex with given object.
func NewRWMutex[T any](value T) (m RWMutex[T]) {
	m.value = value
	// nolint:nakedret
	return
}

// Lock returns an iterator which locks the mutex and yields the guarded object.
// The mutex is unlocked when the iterator is done.
// If the mutex is nil, the iterator is a no-op.
func (m *RWMutex[T]) Lock() iter.Seq[T] {
	return func(yield func(val T) bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		_ = yield(m.value)
	}
}

// RLock returns an iterator which locks the mutex FOR READ and yields the guarded object.
// The mutex is unlocked when the iterator is done.
// If the mutex is nil, the iterator is a no-op.
func (m *RWMutex[T]) RLock() iter.Seq[T] {
	return func(yield func(val T) bool) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		_ = yield(m.value)
	}
}

// version of the value stored in an atomic watch.
type version[T any] struct {
	updated chan struct{}
	value   T
}

// newVersion constructs a new active version.
func newVersion[T any](value T) *version[T] {
	return &version[T]{make(chan struct{}), value}
}

type atomicWatch[T any] struct {
	ptr atomic.Pointer[version[T]]
}

type AtomicSend[T any] struct{ atomicWatch[T] }

func (w *AtomicSend[T]) Subscribe() AtomicRecv[T] {
	return AtomicRecv[T]{&w.atomicWatch}
}

// NewAtomicWatch creates a new AtomicWatch with the given initial value.
func NewAtomicSend[T any](value T) (w AtomicSend[T]) {
	w.ptr.Store(newVersion(value))
	// nolint:nakedret
	return
}

// Store updates the value of the atomic watch.
func (w *AtomicSend[T]) Store(value T) {
	close(w.ptr.Swap(newVersion(value)).updated)
}

// AtomicRecv is a read-only reference to AtomicWatch.
type AtomicRecv[T any] struct{ *atomicWatch[T] }

// Load returns the current value of the atomic watch.
// Does not do any locking.
func (w *atomicWatch[T]) Load() T { return w.ptr.Load().value }

// Wait waits for the value of the atomic watch to satisfy the predicate.
// Does not do any locking.
func (w *atomicWatch[T]) Wait(ctx context.Context, pred func(T) bool) (T, error) {
	for {
		v := w.ptr.Load()
		if pred(v.value) {
			return v.value, nil
		}
		select {
		case <-ctx.Done():
			return Zero[T](), ctx.Err()
		case <-v.updated:
		}
	}
}

// Iter executes sequentially the function f on each value of the atomic watch.
// Context passed to f is canceled when the next value is available.
// Exits when the returned error is different from nil and context.Canceled,
// or when the context passed to Iter is canceled (after f exits).
func (w *atomicWatch[T]) Iter(ctx context.Context, f func(ctx context.Context, v T) error) error {
	for ctx.Err() == nil {
		v := w.ptr.Load()
		g, ctx := errgroup.WithContext(ctx)
		g.Go(func() error { return f(ctx, v.value) })
		g.Go(func() error {
			select {
			case <-ctx.Done():
			case <-v.updated:
			}
			return context.Canceled
		})
		if err := IgnoreCancel(g.Wait()); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// WatchCtrl controls the locked object in a Watch.
// It is provided only in the iterator returned by Lock().
// Should NOT be stored anywhere.
type WatchCtrl struct {
	mu      sync.Mutex
	updated chan struct{}
}

// Watch stores a value of type T.
// Essentially a mutex, that can be awaited for updates.
type Watch[T any] struct {
	ctrl WatchCtrl
	val  T
}

// NewWatch constructs a new watch with the given value.
// Note that value in the watch cannot be changed, so T
// should be a pointer type if updates are required.
func NewWatch[T any](val T) Watch[T] {
	return Watch[T]{
		WatchCtrl{updated: make(chan struct{})},
		val,
	}
}

// Wait waits for the value in the watch to be updated.
// Should be called only after locking the watch, i.e. within Lock() iterator.
// It unlocks -> waits for the update -> locks again.
func (c *WatchCtrl) Wait(ctx context.Context) error {
	updated := c.updated
	c.mu.Unlock()
	defer c.mu.Lock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-updated:
		return nil
	}
}

// WaitUntil waits for the value in the watch to satisfy the predicate.
// Should be called only after locking the watch, i.e. within Lock() iterator.
// The predicate is evaluated under the lock, so it can access the guarded object.
func (c *WatchCtrl) WaitUntil(ctx context.Context, pred func() bool) error {
	for !pred() {
		if err := c.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Updated signals waiters that the value in the watch has been updated.
func (c *WatchCtrl) Updated() {
	close(c.updated)
	c.updated = make(chan struct{})
}

// Lock returns an iterator which locks the watch and yields the guarded object.
// The watch is unlocked when the iterator is done.
// If the watch is nil, the iterator is a no-op.
// Additionally the WatchCtrl object is provided to the yield function:
// * to unlock -> wait for the update -> lock again, call ctrl.Wait(ctx)
// * to signal an update, call ctrl.Updated().
func (w *Watch[T]) Lock() iter.Seq2[T, *WatchCtrl] {
	return func(yield func(val T, ctrl *WatchCtrl) bool) {
		w.ctrl.mu.Lock()
		defer w.ctrl.mu.Unlock()
		_ = yield(w.val, &w.ctrl)
	}
}

// MonitorWatchUpdates calls f and checks if it has updated the watch.
func MonitorWatchUpdates[T any](w *Watch[T], f func()) bool {
	w.ctrl.mu.Lock()
	updated := w.ctrl.updated
	w.ctrl.mu.Unlock()
	f()
	select {
	case <-updated:
		return true
	default:
		return false
	}
}
