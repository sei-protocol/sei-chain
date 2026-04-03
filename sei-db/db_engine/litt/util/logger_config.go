package util

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

type LogFormat string

const (
	JSONLogFormat LogFormat = "json"
	TextLogFormat LogFormat = "text"
)

type LoggerConfig struct {
	Format       LogFormat
	OutputWriter io.Writer
	HandlerOpts  slog.HandlerOptions
}

func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       JSONLogFormat,
		OutputWriter: os.Stdout,
		HandlerOpts: slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
		},
	}
}

func DefaultTextLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       TextLogFormat,
		OutputWriter: os.Stdout,
		HandlerOpts: slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
		},
	}
}

func DefaultSilentLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       TextLogFormat,
		OutputWriter: io.Discard,
	}
}

func DefaultConsoleLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Format:       TextLogFormat,
		OutputWriter: os.Stdout,
		HandlerOpts: slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
		},
	}
}

func NewLogger(cfg *LoggerConfig) (*slog.Logger, error) {
	if cfg.Format == JSONLogFormat {
		return slog.New(slog.NewJSONHandler(cfg.OutputWriter, &cfg.HandlerOpts)), nil
	}
	if cfg.Format == TextLogFormat {
		return slog.New(slog.NewTextHandler(cfg.OutputWriter, &cfg.HandlerOpts)), nil
	}
	return nil, fmt.Errorf("unknown log format: %s", cfg.Format)
}

func SilentLogger() *slog.Logger {
	logger, err := NewLogger(DefaultSilentLoggerConfig())
	if err != nil {
		panic("failed to create silent logger: " + err.Error())
	}
	return logger
}
