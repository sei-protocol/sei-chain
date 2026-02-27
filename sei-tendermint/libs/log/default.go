package log

import (
	"fmt"
	"log/slog"
	"os"
)

var _ Logger = (*defaultLogger)(nil)

type defaultLogger struct {
	logger *slog.Logger
}

// NewDefaultLogger returns a default logger that can be used within Tendermint
// and that fulfills the Logger interface. The underlying logging provider is a
// zerolog logger that supports typical log levels along with JSON and plain/text
// log formats.
//
// Since zerolog supports typed structured logging and it is difficult to reflect
// that in a generic interface, all logging methods accept a series of key/value
// pair tuples, where the key must be a string.
func NewDefaultLogger(format, level string) (Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("failed to parse log level (%s): %w", level, err)
	}
	options := &slog.HandlerOptions{
		Level: lvl,
	}
	var handler slog.Handler
	switch format {
	case LogFormatPlain, LogFormatText:
		handler = slog.NewTextHandler(os.Stderr, options)
	case LogFormatJSON:
		handler = slog.NewJSONHandler(os.Stderr, options)
	default:
		return nil, fmt.Errorf("unsupported log format: %s", format)
	}

	return &defaultLogger{
		logger: slog.New(handler),
	}, nil
}

func (l defaultLogger) Info(msg string, keyVals ...interface{}) {
	l.logger.Info(msg, keyVals...)
}

func (l defaultLogger) Error(msg string, keyVals ...interface{}) {
	l.logger.Error(msg, keyVals...)
}

func (l defaultLogger) Debug(msg string, keyVals ...interface{}) {
	l.logger.Debug(msg, keyVals...)
}

func (l defaultLogger) With(keyVals ...interface{}) Logger {
	return &defaultLogger{
		logger: l.logger.With(keyVals...),
	}
}
