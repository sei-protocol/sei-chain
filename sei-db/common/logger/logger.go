package logger

import (
	"fmt"
	"strings"
)

// Logger is what any CometBFT library should take.
type Logger interface {
	Debug(msg string, keyvals ...interface{})
	Info(msg string, keyvals ...interface{})
	Error(msg string, keyvals ...interface{})
}

type nopLogger struct{}

var _ Logger = (*nopLogger)(nil)
var _ Logger = (*consoleLogger)(nil)

// NewNopLogger returns a logger that doesn't do anything.
func NewNopLogger() Logger { return &nopLogger{} }

func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Debug(string, ...interface{}) {}
func (nopLogger) Error(string, ...interface{}) {}

type consoleLogger struct{}

// NewConsoleLogger returns a logger that prints to stdout.
func NewConsoleLogger() Logger { return &consoleLogger{} }

func (consoleLogger) Debug(msg string, keyvals ...interface{}) {
	fmt.Println("[DEBUG]", msg, formatKeyvals(keyvals))
}

func (consoleLogger) Info(msg string, keyvals ...interface{}) {
	fmt.Println("[INFO]", msg, formatKeyvals(keyvals))
}

func (consoleLogger) Error(msg string, keyvals ...interface{}) {
	fmt.Println("[ERROR]", msg, formatKeyvals(keyvals))
}

func formatKeyvals(keyvals []interface{}) string {
	if len(keyvals) == 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i+1 < len(keyvals); i += 2 {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%v=%v", keyvals[i], keyvals[i+1])
	}
	if len(keyvals)%2 != 0 {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%v=<missing>", keyvals[len(keyvals)-1])
	}
	return sb.String()
}
