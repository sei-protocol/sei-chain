//go:build littdb_wip

package util

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

// LoggerConfig is the configuration for a logger. Contains complex types, so do not embed directly
// in config structs.
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

// NewLogger constructs a logging.Logger from the supplied config.
func NewLogger(cfg *LoggerConfig) (logging.Logger, error) {
	if cfg.Format == JSONLogFormat {
		return logging.NewJsonSLogger(cfg.OutputWriter, &cfg.HandlerOpts), nil
	}
	if cfg.Format == TextLogFormat {
		return logging.NewTextSLogger(cfg.OutputWriter, &cfg.HandlerOpts), nil
	}
	return nil, fmt.Errorf("unknown log format: %s", cfg.Format)
}
