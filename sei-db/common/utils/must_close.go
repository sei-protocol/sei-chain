package utils

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "common", "utils")

// MustClose closes obj if it becomes unreachable while isClosed() still reports it open, logging an error when it
// does. It is a safety net, not a way to close things: any close it performs is a bug in the code that leaked obj.
// In a test binary it panics instead, so a leak fails the test run.
//
// obj must point to the start of an allocation that has no finalizer. isClosed() and close() receive obj as their
// argument and must not capture it, or obj never becomes unreachable; pass method expressions such as
// (*Store).Close. An object reachable from its own goroutines or from a reference cycle is never closed.
func MustClose[T any](
	obj *T,
	description string,
	isClosed func(*T) bool,
	close func(*T),
) {
	MustCloseE(obj, description, isClosed, func(o *T) error {
		close(o)
		return nil
	})
}

// MustCloseE is MustClose() for a close() that returns an error. A close error is logged.
func MustCloseE[T any](
	obj *T,
	description string,
	isClosed func(*T) bool,
	close func(*T) error,
) {
	runtime.SetFinalizer(obj, func(o *T) {
		if isClosed(o) {
			return
		}
		if testing.Testing() {
			panic(fmt.Sprintf("%s became unreachable without being closed", description))
		}
		logger.Error("object became unreachable without being closed, closing it now", "object", description)
		// Finalizers share one goroutine, and a close may block.
		go func() {
			if err := close(o); err != nil {
				logger.Error("failed to close an unreachable object", "object", description, "err", err)
			}
		}()
	})
}
