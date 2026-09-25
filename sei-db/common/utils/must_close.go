package utils

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"weak"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "common", "utils")

// The deepest call stack recorded for where an object passed to MustClose() was created.
const mustCloseStackDepth = 32

// CloseMarker records whether the object it was issued for by MustClose() has been closed. Copies of a marker
// share its state, so it may be stored by value.
type CloseMarker[T any] struct {
	// The state every copy of this marker shares. Nil for a marker not issued by MustClose().
	state *closeState[T]
}

// closeState is the state a CloseMarker and its copies share.
type closeState[T any] struct {
	// The object this marker was issued for.
	owner weak.Pointer[T]

	// Set by CloseMarker.Close().
	closed atomic.Bool

	// What the object is, named in reports.
	description string

	// Where the object was registered. Recorded only in a test binary.
	createdAt []uintptr
}

// MustClose reports obj as leaked if it becomes unreachable before the returned marker is closed. It logs an error,
// or in a test binary panics naming where obj was created. Every path that closes obj must call the marker's
// Close(). An object reachable from its own running goroutines is never reported.
func MustClose[T any](obj *T, description string) CloseMarker[T] {
	state := &closeState[T]{
		owner:       weak.Make(obj),
		description: description,
	}
	if testing.Testing() {
		createdAt := make([]uintptr, mustCloseStackDepth)
		// Skips runtime.Callers() and MustClose() itself.
		state.createdAt = createdAt[:runtime.Callers(2, createdAt)]
	}
	runtime.AddCleanup(obj, reportIfOpen[T], state)
	return CloseMarker[T]{state: state}
}

// Close records that owner has been closed. owner must be the object this marker was issued for. Idempotent.
func (m CloseMarker[T]) Close(owner *T) {
	state := m.issuedState()
	if weak.Make(owner) != state.owner {
		state.report("was closed through a marker issued for another object")
	}
	state.closed.Store(true)
	// owner must stay reachable until it reads as closed, or its cleanup could run in between.
	runtime.KeepAlive(owner)
}

// IsClosed reports whether Close() has been called.
func (m CloseMarker[T]) IsClosed() bool {
	return m.issuedState().closed.Load()
}

// issuedState returns the marker's shared state, panicking if the marker was not issued by MustClose().
func (m CloseMarker[T]) issuedState() *closeState[T] {
	if m.state == nil {
		panic("close marker was not issued by MustClose()")
	}
	return m.state
}

// reportIfOpen reports the object state was issued for as leaked, unless it has been closed.
func reportIfOpen[T any](state *closeState[T]) {
	if state.closed.Load() {
		return
	}
	state.report("became unreachable without being closed")
}

// report panics with problem and the object's creation stack in a test binary, and logs problem otherwise.
func (s *closeState[T]) report(problem string) {
	if testing.Testing() {
		panic(fmt.Sprintf("%s %s; created at:\n%s", s.description, problem, formatStack(s.createdAt)))
	}
	logger.Error("object "+problem, "object", s.description)
}

// formatStack renders the program counters recorded by runtime.Callers() as one function and file:line per frame.
func formatStack(pcs []uintptr) string {
	var b strings.Builder
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		fmt.Fprintf(&b, "  %s\n    %s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			return b.String()
		}
	}
}
