// Package historical reads MVCC state from the CockroachDB offload store.
package historical

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("historical state not found")

// Key is a string so Lookup is usable as a map key.
type Lookup struct {
	StoreName string
	Key       string
}

// Version is the actual MVCC version that satisfied the lookup; may be
// older than the requested target.
type Value struct {
	Bytes   []byte
	Version int64
}

type Reader interface {
	// Returns ErrNotFound if no row exists at or before targetVersion,
	// or if the latest such row is a tombstone.
	Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error)

	// Skips the value transfer — cheaper than Get for large-value stores
	// (EVM storage, code).
	Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error)

	// Missing or tombstoned pairs are absent from the returned map.
	BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error)

	LastVersion(ctx context.Context) (int64, error)
	Close() error
}
