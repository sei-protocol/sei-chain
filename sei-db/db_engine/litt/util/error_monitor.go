package util

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"

	"github.com/Layr-Labs/eigensdk-go/logging"
)

// ErrorMonitor is a struct that permits the process to "panic" without using the golang panic keyword.
// When there are goroutines that function under the hood that are unable to return errors using the standard pattern,
// this utility provides an elegant way to handle those errors. In such situations, the desirable outcome is for the
// process to report the error and to elegantly spin itself down.
//
// Even though this utility can "panic", it is not the same as the panic that is built into Go. The Panic() method
// should be called in situations where recovery is not possible, i.e. the same situations where one would otherwise
// call golang's panic(). The big difference is that calling Panic() will not result in the process immediately being
// torn down.
type ErrorMonitor struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger logging.Logger

	// callback is called when the Panic() method is called for the first time.
	callback func(error)

	// If this is non-nil, the monitor is either in a "panic" state or a "shutdown" state.
	error atomic.Pointer[error]
}

// NewErrorMonitor creates a new ErrorMonitor struct. Executes the callback function when/if Panic() is called.
// The callback is ignored if it is nil.
func NewErrorMonitor(
	ctx context.Context,
	logger logging.Logger,
	callback func(error)) *ErrorMonitor {

	ctx, cancel := context.WithCancel(ctx)

	return &ErrorMonitor{
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
		callback: callback,
	}
}

// Await waits for a value to be sent on a channel. If the channel sends a value, the value is returned.
// If the Panic() is called before the channel sends a value, an error is returned.
func Await[T any](handler *ErrorMonitor, channel <-chan T) (T, error) {
	select {
	case value := <-channel:
		return value, nil
	case <-handler.ImmediateShutdownRequired():
		var zero T
		return zero, fmt.Errorf("context cancelled")
	}
}

// Send sends a value on a channel. If the value is sent, nil is returned. If the Panic() is called before the value
// is sent, an error is returned.
func Send[T any](handler *ErrorMonitor, channel chan<- any, value T) error {
	select {
	case channel <- value:
		return nil
	case <-handler.ImmediateShutdownRequired():
		return fmt.Errorf("context cancelled")
	}
}

// ImmediateShutdownRequired returns an output channel that is closed when Panic() is called. The channel might also be
// closed if the parent context is cancelled, and so this channel being closed can't be used to infer that we are
// in a panicked state.
func (h *ErrorMonitor) ImmediateShutdownRequired() <-chan struct{} {
	return h.ctx.Done()
}

// IsOk returns true if the ErrorMonitor is in a good state, and false if in a "panic" or "shutdown" state.
// If Panic() was called, the error returned is the error that caused the panic, and does not indicate that
// the call to IsOk() failed. If the Panic() has been called multiple times, the error returned will
// be the first error passed to Panic(). If Panic() has not been called and Shutdown() has not been called,
// the error returned will describe the shutdown.
func (h *ErrorMonitor) IsOk() (bool, error) {
	err := h.error.Load()
	if err != nil {
		return false, *err
	}
	return true, nil
}

// Shutdown causes the ErrorMonitor to enter a "shutdown" state. Causes ImmediateShutdownRequired() to signal.
func (h *ErrorMonitor) Shutdown() {
	err := fmt.Errorf("monitor is shut down")

	// don't overwrite the error if there is already an error stored
	h.error.CompareAndSwap(nil, &err)
}

// Panic time! Something just went very wrong. (╯°□°)╯︵ ┻━┻
func (h *ErrorMonitor) Panic(err error) {
	stackTrace := string(debug.Stack())

	h.logger.Errorf("monitor encountered an unrecoverable error: %v\n%s", err, stackTrace)

	// only store the error if there isn't already an error stored
	firstError := h.error.CompareAndSwap(nil, &err)

	// Always cancel the context, even if this is not the first error. It's possible that the first "error" was
	// actually a shutdown request, and we want to make sure that the context is always cancelled in the event
	// of an unexpected error.
	h.cancel()

	if firstError && h.callback != nil {
		h.callback(err)
	}
}
