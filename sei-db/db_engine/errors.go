package db_engine

import "errors"

// ErrNotFound is returned when a key does not exist.
// It mirrors the underlying engine's not-found behavior but provides a
// consistent error type across different backends.
var ErrNotFound = errors.New("not found")
