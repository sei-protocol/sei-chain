package log

import (
	"log/slog"
	"os"
	"sync"
	"testing"
)

var (
	// reuse the same logger across all tests
	_testingLogger    Logger
	initTestingLogger sync.Once
)

// TestingLogger returns a TMLogger which writes to STDOUT if testing being run
// with the verbose (-v) flag, NopLogger otherwise.
//
// Note that the call to TestingLogger() must be made
// inside a test (not in the init func) because
// verbose flag only set at the time of testing.
func TestingLogger() Logger {
	initTestingLogger.Do(func() {
		if testing.Verbose() {
			_testingLogger = &defaultLogger{
				logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
			}
		} else {
			_testingLogger = NewNopLogger()
		}
	})
	return _testingLogger
}
