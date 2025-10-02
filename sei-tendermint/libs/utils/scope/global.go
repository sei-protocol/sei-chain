package scope

import (
	"context"
)

// GlobalHandle is a handle to a task spawned via SpawnGlobal.
type GlobalHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// SpawnGlobal spawns a task in a global context.
// Use with care, as it is not tied to any scope and must be closed manually by calling Close().
// Can be used as an intermediate step when migrating code to use scopes.
func SpawnGlobal(task func(ctx context.Context) error) *GlobalHandle {
	ctx, cancel := context.WithCancel(context.Background())
	h := &GlobalHandle{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		h.err = task(ctx)
		close(h.done)
	}()
	return h
}

// Done returns a channel that is closed when the task is finished.
func (h *GlobalHandle) Done() <-chan struct{} {
	return h.done
}

// Err returns the task's result if it finished, or nil if it is still running.
// Note that if task succeeded, nil is returned.
func (h *GlobalHandle) Err() error {
	select {
	case <-h.done:
		return h.err
	default:
		return nil
	}
}

// Close cancels the task and waits for it to finish.
// Returns the task's result.
func (h *GlobalHandle) Close() error {
	h.cancel()
	<-h.done
	return h.err
}
