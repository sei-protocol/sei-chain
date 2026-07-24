package snapshot

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// ErrEngineClosed is reported (wrapped) by methods that observe engine shutdown when the engine
// was closed normally rather than failed. Detect it with errors.Is.
var ErrEngineClosed = errors.New("snapshot engine closed")

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
// There are no recoverable SnapshotEngine errors. Any error returned by the engine is fatal: the
// engine is in a failed state, will continue to return errors indefinitely, and the caller is
// expected to halt. In particular, a failed DB read permanently fails the affected key — the
// engine never retries, since a retry that succeeded after the failure was propagated could fork
// the chain.
//
// The configured metadata hash key is reserved for the engine; it must not be written or read
// through the engine's key-value methods (see SnapshotEngineConfig.HashKey).
type SnapshotEngine interface {

	// Get returns the value for the given key at the engine's current (mutable) version, or
	// (nil, false, nil) if not found. On a miss the value is read through from the backing store.
	//
	// It is not safe to mutate the key slice after calling this method, nor the returned value slice.
	Get(key []byte, updateLru bool) ([]byte, bool, error)

	// BatchGet reads the given keys against the current (mutable) version and returns a map, keyed by
	// string(key), of the keys that were found to their values. Not-found keys are absent from the
	// map; a present entry is always a found value (an empty value is a non-nil zero-length slice).
	//
	// If any read fails, BatchGet returns a nil map and that error — reads are not partially
	// recoverable. It is not safe to mutate the returned key or value slices.
	BatchGet(keys [][]byte) (map[string][]byte, error)

	// Set writes the value for the given key into the current (mutable) version.
	Set(key []byte, value []byte)

	// Delete removes the given key from the current (mutable) version.
	Delete(key []byte)

	// BatchSet applies the given changeset pairs to the current (mutable) version. A pair with
	// Delete set removes the key; otherwise its Value is written (an empty, non-nil Value is a
	// zero-length value, distinct from a delete).
	BatchSet(updates []*proto.KVPair) error

	// Commit seals the current version as an immutable, point-in-time Snapshot and advances the
	// engine to a fresh mutable version. The returned Snapshot is safe to read for as long as the
	// caller holds a reservation on it; see Snapshot for the full lifecycle contract.
	//
	// Commit must not be called concurrently with operations on the current (mutable)
	// version — Get, BatchGet, Set, Delete, or BatchSet. Reads of previously sealed snapshots
	// may proceed concurrently with it.
	//
	// Commit may block for backpressure when the underlying DB cannot keep up with flushing
	// (see SnapshotEngineConfig.MaxUnflushedVersions). The engine imposes no bound on unhashed
	// or unreleased snapshots, each of which is retained in memory; the caller is responsible
	// for pausing execution when hashing or release falls behind.
	Commit() (Snapshot, error)

	// InitialHash returns the most recently flushed hash as read from the underlying DB when the
	// engine was opened, or nil if the DB had never been flushed. It lets a consumer recover the
	// last persisted hash across restarts. It reflects open-time state and does not change as new
	// snapshots are hashed and flushed.
	InitialHash() []byte

	// Close shuts the engine down. When it returns, the engine's background goroutines have
	// exited and every caller blocked in an engine or snapshot method has been released with
	// either a real result or an error wrapping ErrEngineClosed (or the engine's fatal error).
	//
	// Close does not flush (unflushed snapshot data is recovered upstream via WAL replay) and
	// does not close the injected database or thread pools; the caller owns those and must tear
	// them down after the engine, pools before the database. Close is idempotent: repeat calls
	// return the first result, which is the latched fatal error if the engine failed.
	Close() error
}

// Snapshot is an immutable, point-in-time, read-only view of the data in the engine,
// produced by SnapshotEngine.Commit(). It is safe to read for as long as the caller holds a
// reservation on it.
//
// Consumer responsibilities, in order:
//  1. SetHash must be called exactly once, by a consumer that holds a reservation, to attach
//     the snapshot's content hash.
//  2. Every reservation must be released — the implicit one held by the caller of Commit(),
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

	// BatchGet reads the given keys from the snapshot and returns a map, keyed by string(key), of the
	// keys that were found to their values. Not-found keys are absent from the map; a present entry
	// is always a found value (an empty value is a non-nil zero-length slice).
	//
	// If any read fails, BatchGet returns a nil map and that error — reads are not partially
	// recoverable. It is not safe to mutate the returned key or value slices.
	BatchGet(keys [][]byte) (map[string][]byte, error)

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
	// each reservation, including the one held implicitly by the caller of SnapshotEngine.Commit().
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
	// Returns an error if ctx is cancelled or the engine shuts down before the hash becomes
	// available. Per the hashing-duty contract on Snapshot, the hash is guaranteed to be set
	// before the snapshot's final Release, so callers that themselves hold a reservation can
	// safely block on this.
	AwaitHash(ctx context.Context) ([]byte, error)

	// Returns an iterator over the snapshot's data. Iterator walks data in ascending lexographical order of keys.
	//
	// The engine's reserved metadata hash key (see SnapshotEngineConfig.HashKey) is excluded
	// from iteration: iterator output is exactly the user data at this snapshot's version.
	// Consumers that need the snapshot's hash should use AwaitHash, which is guaranteed to
	// match the iterated data.
	//
	// WARNING: failure to close the iterator may lead to a fatal leak.
	Iterator() Iterator

	// AwaitFlush blocks until the snapshot's data has been written to disk, returning nil once
	// the flush has completed. Returns an error if ctx is cancelled or the engine shuts down
	// first.
	//
	// The caller must hold a reservation across this call. A retired snapshot is no longer
	// recognized by the engine, so AwaitFlush on it returns an error — not success — even
	// though retirement implies the flush completed. Holding a reservation prevents
	// retirement and makes the wait well-defined.
	//
	// Cancelling ctx stops the wait; it has no effect on the flush itself, which proceeds
	// regardless. A ctx error therefore says nothing about flush state: if completion and
	// cancellation become observable simultaneously, either outcome may be returned.
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
