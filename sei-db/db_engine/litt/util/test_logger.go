package util

import (
	"log/slog"
	"os"
)

// GetLogger returns a logger for use in tests.
// The logger always includes source information and logs at debug level.
func GetLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
}
