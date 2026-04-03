package test

import (
	"io"
	"log/slog"
	"os"

	"github.com/Layr-Labs/eigensdk-go/logging"
)

// GetLogger returns a logger for use in tests.
// The logger always includes source information and logs at debug level.
//
// TODO: Future improvements like writing the test output to a file
// and adding test metadata (e.g. test name) to log entries.
func GetLogger() logging.Logger {
	writer := io.Writer(os.Stdout)

	return logging.NewTextSLogger(writer, &logging.SLoggerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
		NoColor:   false,
	})
}
