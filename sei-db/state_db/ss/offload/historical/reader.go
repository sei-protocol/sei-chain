// Package historical serves point-in-time historical state queries from the
// CockroachDB-backed offload store written by the consumer package. It is
// intended for read-heavy workloads such as debug_traceTransaction, which
// resolves thousands of (store, key, version) tuples per request.
package historical

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Get when no row exists for (storeName, key) at
// or before the target version, or when the latest such row is a tombstone.
var ErrNotFound = errors.New("historical state not found")

// Lookup names a (store, key) pair the caller wants resolved at a target
// version. Key uses string for byte content so Lookup can be a map key — the
// usual []byte-as-string idiom is fine because the bytes are immutable.
type Lookup struct {
	StoreName string
	Key       string
}

// Value is the resolved row at or before the requested target version.
// Version reports the actual MVCC version that satisfied the lookup, which
// may be older than the requested target. Tombstones are not surfaced via
// Value: the reader collapses them to absence at the API boundary.
type Value struct {
	Bytes   []byte
	Version int64
}

// Reader serves historical state queries from the offload store. Reads are
// snapshot-consistent and may be served by follower replicas when configured
// (see CockroachConfig.FollowerReadStaleness).
type Reader interface {
	// Get returns the row at the latest version <= targetVersion. Returns
	// ErrNotFound if no such row exists or if the latest row is a tombstone.
	Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error)

	// BatchGet resolves many (store, key) pairs at the same target version in
	// a single round-trip. Pairs that don't exist at or before the target
	// version, or whose latest row is a tombstone, are absent from the
	// returned map. This is the primary API for trace-style workloads where
	// per-key round-trips would dominate latency.
	BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error)

	// LastVersion returns the largest version successfully ingested by the
	// offload consumer. Trace clients should clamp targetVersion to this so
	// they don't query versions still in flight on the Kafka side.
	LastVersion(ctx context.Context) (int64, error)

	Close() error
}
