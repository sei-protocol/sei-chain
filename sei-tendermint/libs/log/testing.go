package log

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
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
	level := LogLevelError
	if testing.Verbose() {
		level = LogLevelDebug
	}
	logger, err := NewTestingLoggerWithLevel(&testingWriter{t}, level)
	require.NoError(t, err)
	return logger
}

// NewTestingLoggerWithLevel creates a testing logger instance at a
// specific level that wraps the behavior of testing.T.Log().
func NewTestingLoggerWithLevel(out io.Writer, level string) (Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	return &defaultLogger{
		logger: slog.New(
			slog.NewTextHandler(
				out,
				&slog.HandlerOptions{Level: lvl})),
	}, nil
}

type testingWriter struct {
	t testing.TB
}

func (tw testingWriter) Write(in []byte) (int, error) {
	tw.t.Log(string(in))
	return len(in), nil
}
