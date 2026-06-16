package litt

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RuntimeConfig holds the non-serializable runtime dependencies for a litt.DB. These are kept separate from
// Config so that Config can be safely deserialized from a config file. Callers that do not want to set every
// field should start from DefaultRuntimeConfig() and override as needed, as the required fields must be set.
type RuntimeConfig struct {
	// The context for the database.
	CTX context.Context

	// The logger for the database.
	Logger *slog.Logger

	// The time source used by the database. This can be substituted for an artificial time source
	// for testing purposes.
	Clock func() time.Time

	// A function that is called if the database experiences a non-recoverable error (e.g. data corruption,
	// a crashed goroutine, a full disk, etc.). This field is optional; if nil, no callback is called. If
	// called at all, this method is called exactly once.
	FatalErrorCallback func(error)
}

// DefaultRuntimeConfig returns a RuntimeConfig with sane default values. Callers that do not want to set
// every field themselves should generally start from this.
func DefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		CTX:    context.Background(),
		Logger: slog.Default(),
		Clock:  time.Now,
	}
}

// Validate returns an error if any required field is unset. FatalErrorCallback is optional and may be nil.
func (r *RuntimeConfig) Validate() error {
	if r.CTX == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if r.Logger == nil {
		return fmt.Errorf("logger cannot be nil")
	}
	if r.Clock == nil {
		return fmt.Errorf("clock cannot be nil")
	}
	return nil
}
