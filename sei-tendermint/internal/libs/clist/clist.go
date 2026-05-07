package clist

/*

The purpose of CList is to provide a goroutine-safe linked-list.
This list can be traversed concurrently by any number of goroutines.
However, removed CElements cannot be added back.
NOTE: Not all methods of container/list are (yet) implemented.
NOTE: Removed elements need to DetachPrev or DetachNext consistently
to ensure garbage collection of removed elements.

*/

import (
	"context"
	"errors"
	"sync"
)

/*
CElement is an element of a linked-list
Traversal from a CElement is goroutine-safe.

We can't avoid using WaitGroups or for-loops given the documentation
spec without re-implementing the primitives that already exist in
golang/sync. Notice that WaitGroup allows many go-routines to be
simultaneously released, which is what we want. Mutex doesn't do
this. RWMutex does this, but it's clumsy to use in the way that a
WaitGroup would be used -- and we'd end up having two RWMutex's for
prev/next each, which is doubly confusing.

sync.Cond would be sort-of useful, but we don't need a write-lock in
the for-loop. Use sync.Cond when you need serial access to the
"condition". In our case our condition is if `next != nil || removed`,
and there's no reason to serialize that condition for goroutines
waiting on NextWait() (since it's just a read operation).
*/
type CElement[T any] struct {
	mtx        sync.RWMutex
	// element mutex protected:
	next       *CElement[T]
	nextWaitCh chan struct{}
	removed    bool

	// list mutext protected:
	prev       *CElement[T]

	// immutable
	value T 
}

var ErrRemoved = errors.New("element was removed")

// Blocking implementation of Next().
// May return ErrRemoved iff CElement was tail and got removed.
func (e *CElement[T]) NextWait(ctx context.Context) (*CElement[T], error) {
	for {
		e.mtx.RLock()
		next := e.next
		removed := e.removed
		signal := e.nextWaitCh
		e.mtx.RUnlock()

		if next != nil {
			return next, nil
		}
		if removed {
			return nil, ErrRemoved
		}

		select {
		case <-signal:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// e.next doesn't necessarily exist here.
		// That's why we need to continue a for-loop.
	}
}

// Nonblocking, may return nil if at the end.
func (e *CElement[T]) Next() *CElement[T] {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return e.next
}

func (e *CElement[T]) getRemoved() bool {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return e.removed
}

func (e *CElement[T]) Value() T {
	return e.value
}

// NOTE: This function needs to be safe for
// concurrent goroutines waiting on nextWg.
func (e *CElement[T]) setNext(newNext *CElement[T]) {
	e.mtx.Lock()

	oldNext := e.next
	e.next = newNext
	if oldNext != nil && newNext == nil {
		// See https://golang.org/pkg/sync/:
		//
		// If a WaitGroup is reused to wait for several independent sets of
		// events, new Add calls must happen after all previous Wait calls have
		// returned.
		e.nextWaitCh = make(chan struct{})
	}
	if oldNext == nil && newNext != nil {
		close(e.nextWaitCh)
	}
	e.mtx.Unlock()
}

func (e *CElement[T]) setRemoved() {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	e.removed = true
	// This wakes up anyone waiting.
	if e.next == nil {
		close(e.nextWaitCh)
	}
	e.prev = nil
}

//--------------------------------------------------------------------------------

// CList represents a linked list.
// The zero value for CList is an empty list ready to use.
// Operations are goroutine-safe.
type CList[T any] struct {
	mtx    sync.RWMutex
	waitCh chan struct{}
	head   *CElement[T] // first element
	tail   *CElement[T] // last element
	len    int          // list length
}

func New[T any]() *CList[T] {
	return &CList[T]{
		waitCh: make(chan struct{}),
		head:   nil,
		tail:   nil,
		len:    0,
	}
}

func (l *CList[T]) Len() int {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	return l.len
}

func (l *CList[T]) Front() *CElement[T] {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	return l.head
}

func (l *CList[T]) WaitFront(ctx context.Context) (*CElement[T], error) {
	// Loop until the head is non-nil else wait and try again
	for {
		l.mtx.RLock()
		head := l.head
		signal := l.waitCh
		l.mtx.RUnlock()

		if head != nil {
			return head, nil
		}
		select {
		case <-signal:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// NOTE: If you think l.head exists here, think harder.
	}
}

func (l *CList[T]) Back() *CElement[T] {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	return l.tail
}

// Panics if list grows beyond its max length.
func (l *CList[T]) PushBack(v T) *CElement[T] {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	// Construct a new element
	e := &CElement[T]{
		prev:       nil,
		next:       nil,
		nextWaitCh: make(chan struct{}),
		removed:    false,
		value:      v,
	}

	// Release waiters on FrontWait/BackWait maybe
	if l.len == 0 {
		close(l.waitCh)
	}
	l.len++

	// Modify the tail
	if l.tail == nil {
		l.head = e
		l.tail = e
	} else {
		e.prev = l.tail // We must init e first.
		l.tail.setNext(e) // This will make e accessible.
		l.tail = e        // Update the list.
	}
	return e
}

// CONTRACT: Caller must call e.DetachPrev() and/or e.DetachNext() to avoid memory leaks.
// NOTE: As per the contract of CList, removed elements cannot be added back.
func (l *CList[T]) Remove(e *CElement[T]) T {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if e.getRemoved() { return e.value }
	next := e.Next()
	
	// Update l.len
	l.len--
	// If we're removing the only item, make CList FrontWait wait.
	if l.len == 0 {
		l.waitCh = make(chan struct{})
	}

	// Connect next/prev and set head/tail
	if e.prev == nil {
		l.head = next
	} else {
		e.prev.setNext(next)
	}
	if next == nil {
		l.tail = e.prev
	} else {
		next.prev = e.prev
	}

	// Set .Done() on e, otherwise waiters will wait forever.
	e.setRemoved()

	return e.value
}
