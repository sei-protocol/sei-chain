package common

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/Layr-Labs/eigensdk-go/logging"
)

type LogFormat string

const (
	JSONLogFormat LogFormat = "json"
	TextLogFormat LogFormat = "text"
)

// Configuration for a logger. Contains complex types, so do not embed directly in config structs.
// If you need a struct to embed in config, use SimpleLoggerConfig instead.
type LoggerConfig struct {
	Format       LogFormat
	OutputWriter io.Writer
	HandlerOpts  logging.SLoggerOptions
}

// DefaultLoggerConfig returns a LoggerConfig with the default settings for a JSON logger.
// In general, this should be the baseline config for most services running in production.
func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       JSONLogFormat,
		OutputWriter: os.Stdout,
		HandlerOpts: logging.SLoggerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
			NoColor:   true,
		},
	}
}

// DefaultTextLoggerConfig returns a LoggerConfig with the default settings for a text logger.
// For use in tests or other scenarios where the logs are consumed by humans.
func DefaultTextLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       TextLogFormat,
		OutputWriter: os.Stdout,
		HandlerOpts: logging.SLoggerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
			NoColor:   true, // color is nice in the console, but not nice when written to a file
		},
	}
}

// DefaultSilentLoggerConfig returns a LoggerConfig that discards all log messages.
// This is useful in tests where you want to suppress log output.
func DefaultSilentLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		// Still set the log format so that we can call NewLogger without error.
		Format:       TextLogFormat,
		OutputWriter: io.Discard,
	}
}

// DefaultConsoleLoggerConfig returns a LoggerConfig with the default settings
// for logging to a console (i.e. with human eyeballs). Adds color, and so should
// not be used when logs are captured in a file.
func DefaultConsoleLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       TextLogFormat,
		OutputWriter: os.Stdout,
		HandlerOpts: logging.SLoggerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
			NoColor:   false,
		},
	}
}

func NewLogger(cfg *LoggerConfig) (logging.Logger, error) {
	if cfg.Format == JSONLogFormat {
		return logging.NewJsonSLogger(cfg.OutputWriter, &cfg.HandlerOpts), nil
	}
	if cfg.Format == TextLogFormat {
		return logging.NewTextSLogger(cfg.OutputWriter, &cfg.HandlerOpts), nil
	}
	return nil, fmt.Errorf("unknown log format: %s", cfg.Format)
}

// SilentLogger returns a logging.Logger that discards all log messages.
func SilentLogger() logging.Logger {
	logger, err := NewLogger(DefaultSilentLoggerConfig())
	if err != nil {
		// This should never happen, since DefaultSilentLoggerConfig always returns a valid config.
		panic("failed to create silent logger: " + err.Error())
	}
	return logger
}
