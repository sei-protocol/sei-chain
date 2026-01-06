package db_engine

import "errors"

// ErrNotFound is returned when a key does not exist.
// It mirrors the underlying engine's not-found behavior but provides a
// consistent error type across different backends.
var ErrNotFound = errors.New("not found")

// IsNotFound returns true if the error represents a "not found" condition.
// It uses errors.Is to check for wrapped ErrNotFound values.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
