package view

import (
	"context"
	"errors"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// ErrViewManagerClosed is reported (wrapped) by methods that observe shutdown when the manager was
// closed normally rather than failed. Detect it with errors.Is.
var ErrViewManagerClosed = errors.New("view manager closed")

// ViewManager provides a read-through cache and efficient point-in-time views on top of a basic
// key-value database. It also coordinates writes to the database, since efficient views require
// careful staging of inserts.
//
// Data is asynchronously flushed to disk once it has been finalized and all its reservations
// released. See View for the full lifecycle.
//
// Warning: it is not safe to mutate byte slices (keys or values) passed to or received from the manager.
// The manager is not required to make defensive copies, and so these slices must be treated as immutable.
//
// There are no recoverable ViewManager errors. Any error returned by the manager is fatal, and
// halting is the caller's responsibility: on the first error the caller is expected to stop, because
// continuing on top of state the manager could not vouch for risks forking the chain.
//
// A caller that ignores an error gets nowhere. After the first failure the manager will eventually
// begin returning errors for all future calls, and calls made concurrently with the original failure
// may go either way: a real response, or a second-hand error inherited from that earlier failure.
// This is a consequence of failing, not a service the manager offers — there is no deadline by which
// it converges and no promise about which error any particular call receives. Do not treat it as a
// safety net for skipping the halt.
//
// The configured metadata key prefix is reserved for the manager; keys under it must not be written
// or read through the manager's key-value methods (see ViewManagerConfig.ReservedPrefix).
type ViewManager interface {

	// Name identifies this manager instance (see ViewManagerConfig.Name). Constant for the
	// manager's lifetime.
	Name() string

	// Get returns the value for the given key at the manager's current (mutable) version, or
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

	// Set writes the value for the given key into the current (mutable) version. Not visible to
	// iterators created earlier (see Iterator).
	Set(key []byte, value []byte) error

	// Delete removes the given key from the current (mutable) version. Not visible to iterators
	// created earlier (see Iterator).
	Delete(key []byte) error

	// BatchSet applies the given changeset pairs to the current (mutable) version. A pair with
	// Delete set removes the key; otherwise its Value is written (an empty, non-nil Value is a
	// zero-length value, distinct from a delete). Not visible to iterators created earlier (see
	// Iterator).
	BatchSet(updates []*proto.KVPair) error

	// Commit seals the current version as an immutable, point-in-time View and advances the
	// manager to a fresh mutable version. The returned View is safe to read for as long as the
	// caller holds a reservation on it; see View for the full lifecycle contract.
	//
	// Commit must not be called concurrently with operations on the current (mutable)
	// version — Get, BatchGet, Set, Delete, BatchSet, or the construction of an Iterator. Reads of
	// sealed views may proceed concurrently with it, and so may reads through an already-constructed
	// Iterator: an iterator is fixed at its creation instant, so a seal cannot disturb it.
	//
	// Commit may block for backpressure when the underlying DB cannot keep up with flushing
	// (see ViewManagerConfig.MaxUnflushedVersions). The manager imposes no bound on unfinalized
	// or unreleased views, each of which is retained in memory; the caller is responsible
	// for pausing execution when finalization or release falls behind.
	Commit() (View, error)

	// Iterator returns an iterator over the manager's current (mutable) version, restricted to
	// opts.LowerBound (inclusive) and opts.UpperBound (exclusive) and walking keys in descending
	// lexicographical order when opts.Reverse is set, ascending otherwise. A nil opts means the whole
	// keyspace, ascending. Keys under the manager's reserved metadata prefix are excluded (see
	// ViewManagerConfig.ReservedPrefix).
	//
	// The returned iterator is fixed at the instant it was constructed, and stays usable for as long
	// as it is held: its in-memory overrides are a private copy and its read of the backing database
	// is pinned, so writes, seals, flushes and retirement that follow cannot change what it returns.
	// Equally, it will never show them — a caller that wants later writes needs a new iterator.
	// Holding one is therefore safe from another thread, and does not block writes.
	//
	// Constructing an iterator must NOT race a BatchSet. Each shard's overrides are copied under that
	// shard's own lock, so a batch spanning two shards during construction can leave the iterator
	// holding part of it — a state belonging to no single instant, reported without an error. Serialize
	// construction against BatchSet. Set and Delete each touch a single shard and so are seen either
	// wholly or not at all; Commit stages no values and cannot be seen at all.
	//
	// An iterator must be closed. It holds resources in the backing database — pinned files that
	// cannot be compacted away — and reading one after the manager has closed is undefined behaviour;
	// Close reports a leak on a best-effort basis (see Close).
	//
	// Returns an error if the iterator cannot be constructed, in which case no iterator is returned
	// and there is nothing to close.
	//
	// The returned iterator is single-pass: it arrives positioned on its first pair, there is no Prev
	// or Seek, and the direction cannot be changed mid-walk. It is not thread-safe and must not be
	// shared across goroutines.
	Iterator(opts *types.IterOptions) (dbm.Iterator, error)

	// EscapeHatchUnderlyingDB returns the raw backing database, bypassing every guarantee this manager
	// provides. The name is deliberately obstructive; see the implementation for why every use except
	// taking a checkpoint is a bug.
	EscapeHatchUnderlyingDB() types.KeyValueDB

	// Releases every blocked caller and schedules for all resources held to be released. This includes
	// closing the underlying database, which the manager owns. When Close returns no manager-owned
	// goroutine will touch that database again. Idempotent.
	//
	// Reading a View or an Iterator produced by this manager after Close is undefined behaviour
	// and the caller must not do it. The manager makes a best-effort attempt to fail such a read
	// rather than answer it with nonsensical data, but that is a consequence of shutting down, not a
	// service: a read that races Close may legitimately return the correct value instead of an error.
	// Do not build synchronization on top of either outcome.
	//
	// Closing while an iterator is still open is reported in the returned error, naming the manager and
	// how many are open, so the leak is legible rather than surfacing later as an error from the
	// storage engine. Close does not wait for iterators, and the count is best-effort: it may be
	// stale.
	Close() error
}

// View is an immutable, point-in-time, read-only view of the data in the manager,
// produced by ViewManager.Commit(). It is safe to read for as long as the caller holds a
// reservation on it.
//
// Consumer responsibilities, in order:
//  1. Finalize must be called exactly once, by a consumer that holds a reservation, to attach the
//     view's metadata (for example its content hash).
//  2. Every reservation must be released — the implicit one held by the caller of Commit(),
//     plus any acquired via Reserve. The final Release must happen after Finalize; releasing
//     the last reservation on an unfinalized view is a fatal error.
//
// Independently of consumer activity, the manager asynchronously performs two cleanup steps in
// version order:
//   - Flushing (writing the view's diff to disk) is possible as soon as Finalize has returned.
//     The manager may flush a view while reservations are still outstanding, as long as it
//     is the oldest unflushed view — that is, flushing can race ahead of the final
//     Release. Outstanding reservations on an older view will, however, block the flush
//     of every newer view until those reservations are released.
//   - Retirement (freeing the view's in-memory state) happens only after the view has
//     been both flushed AND fully released.
type View interface {
	// Name returns the name of the manager this view was taken from.
	Name() string

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

	// BatchGet reads the given keys from the view and returns a map, keyed by string(key), of the
	// keys that were found to their values. Not-found keys are absent from the map; a present entry
	// is always a found value (an empty value is a non-nil zero-length slice).
	//
	// If any read fails, BatchGet returns a nil map and that error — reads are not partially
	// recoverable. It is not safe to mutate the returned key or value slices.
	BatchGet(keys [][]byte) (map[string][]byte, error)

	// GetDiff returns the set of key-value mutations contained in this view, relative to the
	// previous view. The result reflects only this view's writes (including deletes,
	// represented as nil values); to reconstruct earlier state, read from earlier views.
	GetDiff() (map[string][]byte, error)

	// Reserve increments this view's reservation count. While the count is greater than zero,
	// the view is safe to read and its internal data is protected from cleanup. Each Reserve
	// must be paired with exactly one Release.
	//
	// Use Reserve before handing a view to another goroutine or component that will read from
	// it, and have that consumer Release when it is done.
	Reserve() error

	// Release decrements this view's reservation count. It must be called exactly once for
	// each reservation, including the one held implicitly by the caller of ViewManager.Commit().
	//
	// When the final reservation is released, the view must already be finalized (see the
	// finalization-duty contract on View); releasing the final reservation on an unfinalized
	// view is a fatal error. After the final Release, the view is no longer safe to read
	// and its in-memory data becomes eligible for cleanup once it has been flushed to disk.
	//
	// View N must be fully released before view N+1 is eligible to be flushed to disk;
	// failing to release a view will stall flushes of all later views indefinitely.
	Release() error

	// Finalize attaches this view's metadata — whatever the consumer wants recorded alongside
	// the block, such as its content hash. It must be called exactly once, by a consumer that
	// currently holds a reservation, and must return before that consumer issues its final Release
	// (see the finalization-duty contract on View).
	//
	// Every key written must fall under the manager's reserved prefix (see
	// ViewManagerConfig.ReservedPrefix); the manager does not verify this, and writing outside
	// the prefix corrupts user data. The pairs are written to disk in the same atomic batch as the
	// view's diff, so the view is not eligible to be flushed until Finalize has returned.
	//
	// An empty write set is legal: a consumer with nothing to record still has to finalize, because
	// finalization is what makes the view flushable.
	Finalize(writes []*proto.KVPair) error

	// AwaitFlush blocks until the view's data has been written to disk, returning nil once
	// the flush has completed. Returns an error if ctx is cancelled or the manager shuts down
	// first.
	//
	// The caller must hold a reservation across this call; holding one is what stops the view
	// being retired mid-wait. Calling without one is undefined behaviour.
	//
	// Cancelling ctx stops the wait; it has no effect on the flush itself, which proceeds
	// regardless. A ctx error therefore says nothing about flush state: if completion and
	// cancellation become observable simultaneously, either outcome may be returned.
	AwaitFlush(ctx context.Context) error
}
