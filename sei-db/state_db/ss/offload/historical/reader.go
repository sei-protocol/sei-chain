// Package historical reads MVCC state from the CockroachDB offload store.
package historical

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("historical state not found")

// Lookup uses string for Key so it can be a map key (the []byte-as-string idiom).
type Lookup struct {
	StoreName string
	Key       string
}

// Value.Version is the actual MVCC version that satisfied the lookup,
// which may be older than the requested target.
type Value struct {
	Bytes   []byte
	Version int64
}

type Reader interface {
	// Get returns ErrNotFound if no row exists at or before targetVersion,
	// or if the latest such row is a tombstone.
	Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error)

	// Has reports existence without transferring the value bytes — cheaper
	// than Get for stores with large values (EVM storage, code).
	Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error)

	// BatchGet resolves many (store, key) pairs in one round-trip. Missing
	// or tombstoned pairs are absent from the returned map.
	BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error)

	LastVersion(ctx context.Context) (int64, error)
	Close() error
}
