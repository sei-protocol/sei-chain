package proxy

import "sync"

// Turn wakes goroutines waiting on the next Broadcast.
type Turn struct {
	mu sync.Mutex
	ch chan struct{}
}

// Chan returns the channel the next Broadcast closes.
func (t *Turn) Chan() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ch == nil {
		t.ch = make(chan struct{})
	}
	return t.ch
}

// Broadcast wakes every goroutine waiting on the current Chan.
func (t *Turn) Broadcast() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ch != nil {
		close(t.ch)
	}
	t.ch = make(chan struct{})
}
