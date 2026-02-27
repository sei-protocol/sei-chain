package log

import (
	"log/slog"
	"testing"
)

// NewTestingLogger converts a testing.T into a logging interface to
// make test failures and verbose provide better feedback associated
// with test failures. This logging instance is safe for use from
// multiple threads, but in general you should create one of these
// loggers ONCE for each *testing.T instance that you interact with.
//
// By default it collects only ERROR messages, or DEBUG messages in
// verbose mode, and relies on the underlying behavior of
// testing.T.Log()
//
// Users should be careful to ensure that no calls to this logger are
// made in goroutines that are running after (which, by the rules of
// testing.TB will panic.)
func NewTestingLogger(t testing.TB) Logger {
	level := slog.LevelError
	if testing.Verbose() {
		level = slog.LevelDebug
	}
	out := &testingWriter{t}
	return &defaultLogger{
		logger: slog.New(
			slog.NewTextHandler(
				out,
				&slog.HandlerOptions{Level: level})),
	}
}

type testingWriter struct {
	t testing.TB
}

func (tw testingWriter) Write(in []byte) (int, error) {
	tw.t.Log(string(in))
	return len(in), nil
}
