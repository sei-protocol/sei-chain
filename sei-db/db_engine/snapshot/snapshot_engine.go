package snapshot

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// SnapshotEngine provides a read-through cache and efficient point-in-time snapshots on top of a basic
// key-value database. It also coordinates writes to the database, since efficient snapshots require
// careful staging of inserts.
//
// Data is asynchronously flushed to disk once it has been hashed and all its reservations
// released. See Snapshot for the full lifecycle.
//
// Warning: it is not safe to mutate byte slices (keys or values) passed to or received from the engine.
// The engine is not required to make defensive copies, and so these slices must be treated as immutable.
//
// SnapshotEngine errors are generally not recoverable, and it should be assumed that an engine that has
// returned an error is in a corrupted state and should be discarded.
type SnapshotEngine interface {

	// Get returns the value for the given key at the engine's current (mutable) version, or
	// (nil, false, nil) if not found. On a miss the value is read through from the backing store.
	//
	// It is not safe to mutate the key slice after calling this method, nor the returned value slice.
	Get(key []byte, updateLru bool) ([]byte, bool, error)

	// BatchGet performs a batch read against the current (mutable) version. Given a map of keys to
	// read, it performs the reads and updates the map with the results.
	//
	// It is not thread safe to read or mutate the map while this method is running, nor is it safe to
	// mutate the key or value slices in the map after calling this method.
	BatchGet(keys map[string]types.BatchGetResult) error

	// Set writes the value for the given key into the current (mutable) version.
	Set(key []byte, value []byte)

	// Delete removes the given key from the current (mutable) version.
	Delete(key []byte)

	// BatchSet applies the given key-value mutations to the current (mutable) version.
	BatchSet(updates []Mutation) error

	// Snapshot seals the current version as an immutable, point-in-time Snapshot and advances the
	// engine to a fresh mutable version. The returned Snapshot is safe to read for as long as the
	// caller holds a reservation on it; see Snapshot for the full lifecycle contract.
	Snapshot() (Snapshot, error)

	// InitialHash returns the most recently flushed hash as read from the underlying DB when the
	// engine was opened, or nil if the DB had never been flushed. It lets a consumer recover the
	// last persisted hash across restarts. It reflects open-time state and does not change as new
	// snapshots are hashed and flushed.
	InitialHash() []byte

	// Close closes the snapshot engine and the underlying database. Before tearing down, Close flushes
	// whatever snapshots are currently flush-eligible — the contiguous prefix of hashed,
	// unflushed snapshots starting at the oldest, applying the same eligibility rules as the
	// background flusher (see Snapshot). It does NOT wait for unhashed snapshots to
	// receive their hash, nor for outstanding reservations to be released. On a successful
	// return, all flush-eligible snapshot data has been persistently written to disk.
	//
	// It is not safe to call Close() concurrently with any other method on this interface, nor is it safe to call
	// Close() while any snapshots are still in use. It is legal to call Close() even if all snapshot reference
	// counts have not yet reached 0, but those snapshots are no longer safe to read when this method is called.
	Close() error
}

// Snapshot is an immutable, point-in-time, read-only view of the data in the engine,
// produced by SnapshotEngine.Snapshot(). It is safe to read for as long as the caller holds a
// reservation on it.
//
// Consumer responsibilities, in order:
//  1. SetHash must be called exactly once, by a consumer that holds a reservation, to attach
//     the snapshot's content hash.
//  2. Every reservation must be released — the implicit one held by the caller of Snapshot(),
//     plus any acquired via Reserve. The final Release must happen after SetHash; releasing
//     the last reservation on an unhashed snapshot is a fatal error.
//
// The hashing duty: exactly one consumer is responsible for calling SetHash, and that
// consumer must hold a reservation across both the SetHash call and its matching Release.
// This is what enforces ordering between steps 1 and 2 above.
//
// Independently of consumer activity, the engine asynchronously performs two cleanup steps in
// version order:
//   - Flushing (writing the snapshot's diff to disk) is possible as soon as the hash is set.
//     The engine may flush a snapshot while reservations are still outstanding, as long as it
//     is the oldest unflushed snapshot — that is, flushing can race ahead of the final
//     Release. Outstanding reservations on an older snapshot will, however, block the flush
//     of every newer snapshot until those reservations are released.
//   - Retirement (freeing the snapshot's in-memory state) happens only after the snapshot has
//     been both flushed AND fully released.
type Snapshot interface {
	// Get returns the value for the given key, or (nil, false, nil) if not found.
	//
	// It is not safe to mutate the key slice after calling this method, nor is it safe to mutate the value slice
	// that is returned.
	Get(
		// The entry to fetch.
		key []byte,
		// If true, the LRU queue will be updated. If false, the LRU queue will not be updated.
		// Useful for when an operation is performed multiple times in close succession on the same key,
		// since it requires non-zero overhead to do so with little benefit.
		updateLru bool,
	) ([]byte, bool, error)

	// Perform a batch read operation. Given a map of keys to read, performs the reads and updates the
	// map with the results.
	//
	// It is not thread safe to read or mutate the map while this method is running. It is also not safe to mutate the
	// key or value slices in the map after calling this method.
	BatchGet(keys map[string]types.BatchGetResult) error

	// GetDiff returns the set of key-value mutations contained in this snapshot, relative to the
	// previous snapshot. The result reflects only this snapshot's writes (including deletes,
	// represented as nil values); to reconstruct earlier state, read from earlier snapshots.
	GetDiff() (map[string][]byte, error)

	// Reserve increments this snapshot's reservation count. While the count is greater than zero,
	// the snapshot is safe to read and its internal data is protected from cleanup. Each Reserve
	// must be paired with exactly one Release.
	//
	// Use Reserve before handing a snapshot to another goroutine or component that will read from
	// it, and have that consumer Release when it is done.
	Reserve() error

	// Release decrements this snapshot's reservation count. It must be called exactly once for
	// each reservation, including the one held implicitly by the caller of SnapshotEngine.Snapshot().
	//
	// When the final reservation is released, the snapshot's hash must already be set (see the
	// hashing-duty contract on Snapshot); releasing the final reservation on an unhashed
	// snapshot is a fatal error. After the final Release, the snapshot is no longer safe to read
	// and its in-memory data becomes eligible for cleanup once it has been flushed to disk.
	//
	// Snapshot N must be fully released before snapshot N+1 is eligible to be flushed to disk;
	// failing to release a snapshot will stall flushes of all later snapshots indefinitely.
	Release() error

	// SetHash attaches a content hash to this snapshot. It must be called exactly once, by a
	// consumer that currently holds a reservation, and must return before that consumer issues
	// its final Release (see the hashing-duty contract on Snapshot).
	//
	// The hash is written to disk alongside the snapshot's diff, so the snapshot is not eligible
	// to be flushed until SetHash has returned.
	//
	// Does not accept nil hashes.
	SetHash(hash []byte) error

	// AwaitHash blocks until SetHash has been called on this snapshot, then returns the hash.
	// Returns an error if ctx is cancelled before the hash becomes available. Per the hashing-duty
	// contract on Snapshot, the hash is guaranteed to be set before the snapshot's final
	// Release, so callers that themselves hold a reservation can safely block on this.
	AwaitHash(ctx context.Context) ([]byte, error)

	// Returns an iterator over the snapshot's data. Iterator walks data in ascending lexographical order of keys.
	//
	// WARNING: failure to close the iterator may lead to a fatal leak.
	Iterator() Iterator

	// AwaitFlush blocks until the snapshot's data has been written to disk. Returns nil if the
	// flush has completed, even if ctx fires concurrently. Returns an error if ctx is cancelled
	// or the engine is shut down before the flush actually occurs.
	AwaitFlush(ctx context.Context) error
}

// Iterator provides ordered iteration over a snapshot's data. Multiplexes on-disk data with in-memory data.
// Data is traversed in ascending lexographical order of keys. Forward-only; there is no Prev or Seek.
//
// Iterators are not thread-safe. A single iterator must not be shared across goroutines.
type Iterator interface {
	// Next moves the iterator to the next key-value pair.
	//
	// The returned key and value slices are owned by the caller and remain
	// valid until Close. It is not safe to mutate them.
	Next() (
		// Returns true until the iterator is out of data, then false when the iterator is exhausted.
		ok bool,
		// The next key, or nil if the iterator is exhausted.
		key []byte,
		// The next value, or nil if the iterator is exhausted.
		value []byte,
		// An error if the iterator encountered an error. Errors are sticky:
		// once Next returns an error, all subsequent calls return the same
		// error.
		err error,
	)

	// Closes the iterator, releasing held resources. Idempotent.
	//
	// WARNING: failure to close the iterator may lead to a fatal leak.
	Close() error
}

// Mutation describes a single key-value mutation to apply to the snapshot engine.
type Mutation struct {
	// The key to update.
	Key []byte
	// The value to set. If nil, the key will be deleted.
	Value []byte
}

// IsDelete returns true if the update is a delete operation.
func (u *Mutation) IsDelete() bool {
	return u.Value == nil
}
