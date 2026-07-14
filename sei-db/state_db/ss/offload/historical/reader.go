// Package historical reads MVCC state from an external historical store.
package historical

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("historical state not found")

// Value is the actual MVCC value that satisfied the lookup.
// Version may be older than the requested target version.
type Value struct {
	Bytes   []byte
	Version int64
}

type Reader interface {
	// Get returns ErrNotFound if no row exists at or before targetVersion,
	// or if the latest such row is a tombstone.
	Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error)

	// Has skips value transfer and returns false for missing or tombstoned keys.
	Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error)

	LastVersion(ctx context.Context) (int64, error)
	Close() error
}
