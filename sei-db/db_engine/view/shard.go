package view

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// A single shard of a ViewManager. The shard owns the MVCC layer: versioned in-memory data
// awaiting flush, per-version diffs, and version bookkeeping. Reads that miss the versioned data
// fall through to the shard's read-through DB cache (see readCache).
//
// A shard that is out of service refuses reads and writes, reporting the failure that stopped it.
// Two things put it there:
//
//   - The manager was shut down. Only reachable by calling Close concurrently with an operation that
//     touches a shard, which is illegal.
//   - The database crashed. Database failures are fatal and are never recovered from, so every shard
//     goes out of service, not just the one that saw the failure.
//
// Capitalized methods are the surface the ViewManager calls; shard is unexported, so they are not
// exports.
//
// Method postfixes state the lock contract: RLocked and WLocked require the caller to hold the read or
// write lock, and Unlocked requires the caller to hold neither. A bare name touches no guarded state,
// or is external surface whose caller has no access to the lock.
type shard struct {
	// A lock to protect the shard's data. Also used by the read cache (see the cache field).
	lock sync.RWMutex

	// Data at various versions. This is for data that has not yet been flushed down into the DB.
	versionedData map[string] /* key */ *structures.Deque[versionedValue] /* values at various versions */

	// The values set in each version not yet materialized: the current version, and any sealed version
	// whose diff has not been ordered yet. A key set more than once in a version keeps only its last value.
	// Reading one key at a version through versionedData is far cheaper than scanning this map for it.
	versionDiffs map[uint64] /* version */ map[string] /* key */ []byte /* value */

	// One handle per sealed version, carrying that version's writes once they are ordered by key.
	// Registered at the seal, and empty until the version is materialized.
	sealedDiffs map[uint64] /* version */ *sealedDiff

	// The read-through DB cache backing this shard. A passive component sharing this shard's
	// lock: its RLocked and WLocked methods require the lock held, while its Resolve methods and background
	// read-completion paths manage their own synchronization. The cache never calls outward while
	// holding the lock — it never calls into the shard at all, and its one call into the manager
	// (reportReadFailure, which acquires versionLock) is made only after releasing the lock — so
	// nesting cache calls under the shard lock cannot deadlock. See readCache.
	cache *readCache

	// This shard's handle on the ViewManager-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *shardMetrics

	// The current version number.
	currentVersion uint64

	// The oldest version number kept in versionedData.
	oldestVersion uint64

	// The number of iterators currently reading this shard. Close reports a non-zero count as a
	// leaked iterator, since reading one after the database has closed is undefined behaviour (see
	// ViewManager.Close).
	openIterators uint64

	// ctx is cancelled when the manager shuts down, releasing a wait for a sealed version's diff.
	ctx context.Context

	// shutdownError names the cause once ctx is cancelled.
	shutdownError func() error

	// initialVersionsPerKey sizes the value list a key gets when first written. See
	// ViewManagerConfig.InitialVersionsPerKey.
	initialVersionsPerKey int
}

// Write is one key's change: a value to store, or a deletion.
//
// A nil Value is a deletion. That is the manager's tombstone convention throughout — the version diff
// maps hold nil for a deleted key — so a caller with a genuinely empty value passes a non-nil,
// zero-length slice.
//
// Key may be carved from a shared buffer; see setWLocked for what the manager retains.
type Write struct {
	// Key is the key to write.
	Key string

	// Value is the value to store, or nil to delete the key.
	Value []byte
}

// sealedDiff is a sealed version's writes ordered by key. entries is valid only once done is closed;
// before that the version's writes are still in versionDiffs.
type sealedDiff struct {
	entries []Write
	done    chan struct{}
}

// A single value at a specific version.
type versionedValue struct {
	// The value.
	value []byte
	// The manager version at which this value was written. Note that this is NOT the same
	// as block height, this is just a version number that monotonically increases over the lifetime
	// of a view manager instance.
	version uint64
}

// Creates a new Shard.
func NewShard(
	ctx context.Context,
	config *ViewManagerConfig,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for asynchronous reads.
	readPool threading.Pool,
	// The maximum size of this shard, in bytes.
	maxSize uint64,
	// Maps the context cancellation observed by a blocked read to the manager's shutdown error
	// (the latched fatal error, or ErrViewManagerClosed on a clean close). Called only after ctx has
	// been cancelled.
	shutdownError func() error,
	// Reports a failed DB read to the manager, which bricks and stops serving reads.
	reportReadFailure func(error),
) (*shard, error) {

	if maxSize == 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}
	if shutdownError == nil {
		return nil, fmt.Errorf("shutdownError must be non-nil")
	}
	if reportReadFailure == nil {
		// A shard that cannot report a read failure would serve reads after one, which is the
		// failure mode this reporting exists to prevent.
		return nil, fmt.Errorf("reportReadFailure must be non-nil")
	}

	versionDiffs := make(map[uint64]map[string][]byte)
	versionDiffs[1] = make(map[string][]byte) // versions start at 1

	s := &shard{
		versionedData:  make(map[string]*structures.Deque[versionedValue]),
		versionDiffs:   versionDiffs,
		sealedDiffs:    make(map[uint64]*sealedDiff),
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,
		ctx:            ctx,
		shutdownError:  shutdownError,

		initialVersionsPerKey: int(config.InitialVersionsPerKey), //nolint:gosec // validated non-zero
	}
	s.cache = NewReadCache(ctx, config, db, readPool, &s.lock, maxSize, shutdownError, reportReadFailure)
	return s, nil
}

// Get returns the value for the given key, or (nil, false, nil) if not found at the given version.
func (s *shard) Get(
	// The key to get.
	key []byte,
	// The version of the data to get.
	version uint64,
	// If true, the entry's recency is recorded. If false, it is not. Useful for when an operation is
	// performed multiple times in close succession on the same key, since it requires non-zero
	// overhead to do so with little benefit.
	updateLru bool,
) ([]byte, bool, error) {
	value, found, done, err := s.attemptFastGetUnlocked(key, version, updateLru)
	if done {
		if err != nil {
			return nil, false, fmt.Errorf("get at version %d: %w", version, err)
		}
		return value, found, nil
	}

	// Not resolvable without mutating: classify against the DB read-cache under the write lock,
	// then complete the read (which may schedule a DB read and block) outside it. Redone from scratch
	// because the lock was released in between, so another reader may have scheduled this key.
	s.lock.Lock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		s.lock.Unlock()
		return nil, false, fmt.Errorf("get key %x: %w", key, err)
	}

	if err := s.validateVersionRLocked(version); err != nil {
		s.lock.Unlock()
		return nil, false, fmt.Errorf("get key %x: %w", key, err)
	}

	// First, check to see if we have this value in the versioned data map.
	if entry, found := s.lookupVersionedRLocked(key, version); found {
		s.lock.Unlock()
		s.metrics.reportCacheHits(1)
		return entry.value, entry.value != nil, nil
	}

	outcome := s.cache.LookupWLocked(key, updateLru)
	s.lock.Unlock()

	return s.cache.ResolveUnlocked(key, outcome)
}

// attemptFastGetUnlocked attempts a read while holding only the read lock, reporting done when it
// succeeded. A non-nil err always comes with done. A read it could not resolve without mutating is
// left to the caller to redo under the write lock.
func (s *shard) attemptFastGetUnlocked(
	key []byte,
	version uint64,
	updateLru bool,
) (value []byte, found bool, done bool, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, false, true, fmt.Errorf("key %x: %w", key, err)
	}
	if err := s.validateVersionRLocked(version); err != nil {
		return nil, false, true, fmt.Errorf("key %x: %w", key, err)
	}

	if entry, found := s.lookupVersionedRLocked(key, version); found {
		s.metrics.reportCacheHits(1)
		return entry.value, entry.value != nil, true, nil
	}

	value, found, ok := s.cache.AttemptFastLookupRLocked(key, updateLru)
	if !ok {
		return nil, false, false, nil
	}
	s.metrics.reportCacheHits(1)
	return value, found, true, nil
}

// validateVersionRLocked checks that the given version is within the valid range.
func (s *shard) validateVersionRLocked(version uint64) error {
	if version < s.oldestVersion {
		return fmt.Errorf("version (%d) is less than the oldest version (%d)", version, s.oldestVersion)
	}
	if version > s.currentVersion {
		return fmt.Errorf("version (%d) is greater than the current version (%d)", version, s.currentVersion)
	}
	return nil
}

// lookupVersionedRLocked checks versioned data for a key at the given version. Reports the entry and
// true when versioned data holds one, or false when the read cache should be consulted instead.
func (s *shard) lookupVersionedRLocked(key []byte, version uint64) (versionedValue, bool) {
	// Converted inline rather than by the caller: the compiler elides the conversion only where it
	// indexes a map directly, and every single-key read pays an allocation for it otherwise.
	deque, ok := s.versionedData[string(key)]
	if !ok {
		return versionedValue{}, false
	}
	// Scanned from the newest even at oldestVersion: while DropVersions is moving keys, a deque may
	// still hold values older than oldestVersion.
	for i := deque.Len() - 1; i >= 0; i-- {
		next := deque.Get(i)
		if next.version <= version {
			return next, true
		}
	}
	return versionedValue{}, false
}

// BatchGet reads the given keys at the given version, returning a map (keyed by string(key)) of the
// keys that were found to their values. Not-found and deleted keys are absent from the map. Any read
// error fails the whole call and returns a nil map.
func (s *shard) BatchGet(keys [][]byte, version uint64) (map[string][]byte, error) {
	results := make(map[string][]byte, len(keys))

	unresolved, hits, err := s.attemptFastBatchGetUnlocked(keys, results, version)
	if err != nil {
		return nil, fmt.Errorf("batch get of %d keys at version %d: %w", len(keys), version, err)
	}

	var pending []pendingRead
	if len(unresolved) > 0 {
		var remainingHits int64
		pending, remainingHits, err = s.batchGetRemainingUnlocked(keys, unresolved, results, version)
		if err != nil {
			return nil, fmt.Errorf("batch get of %d unresolved keys at version %d: %w",
				len(unresolved), version, err)
		}
		hits += remainingHits
	}

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	if err := s.cache.ResolveBatchUnlocked(pending, results); err != nil {
		// DB errors are fatal; fail the whole batch.
		return nil, fmt.Errorf("complete %d database reads for a batch get: %w", len(pending), err)
	}
	return results, nil
}

// attemptFastBatchGetUnlocked resolves the keys it can while holding the read lock, writing found
// values into results and returning the positions in keys of those it could not resolve.
func (s *shard) attemptFastBatchGetUnlocked(
	keys [][]byte,
	results map[string][]byte,
	version uint64,
) (unresolved []int, hits int64, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, 0, fmt.Errorf("resolve what is already in memory: %w", err)
	}

	if err := s.validateVersionRLocked(version); err != nil {
		return nil, 0, fmt.Errorf("resolve what is already in memory: %w", err)
	}

	for i, key := range keys {
		keyStr := string(key)
		if entry, found := s.lookupVersionedRLocked(key, version); found {
			// found includes tombstones (nil value); only non-nil values are real hits to return.
			if entry.value != nil {
				results[keyStr] = entry.value
			}
			hits++
			continue
		}

		// The batch path never records recency on hits, hence updateLru=false.
		value, found, ok := s.cache.AttemptFastLookupRLocked(key, false)
		if ok {
			// Resolved from cache. A not-found (deleted) key counts as a hit but is not a result.
			if found {
				results[keyStr] = value
			}
			hits++
			continue
		}
		unresolved = append(unresolved, i)
	}
	return unresolved, hits, nil
}

// batchGetRemainingUnlocked classifies the keys at the given positions in keys, which are those the
// fast pass could not resolve, creating entries and scheduling DB reads as needed.
func (s *shard) batchGetRemainingUnlocked(
	keys [][]byte,
	indices []int,
	results map[string][]byte,
	version uint64,
) (pending []pendingRead, hits int64, err error) {
	pending = make([]pendingRead, 0, len(indices))

	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, 0, fmt.Errorf("classify the remaining keys: %w", err)
	}

	if err := s.validateVersionRLocked(version); err != nil {
		return nil, 0, fmt.Errorf("classify the remaining keys: %w", err)
	}

	// Redone from scratch rather than carried over from the fast pass, because the lock was released
	// in between and another reader may have scheduled or completed any of these keys.
	for _, i := range indices {
		key := keys[i]
		keyStr := string(key)
		if entry, found := s.lookupVersionedRLocked(key, version); found {
			if entry.value != nil {
				results[keyStr] = entry.value
			}
			hits++
			continue
		}

		outcome := s.cache.LookupWLocked(key, false)
		if outcome.immediate {
			if outcome.found {
				results[keyStr] = outcome.value
			}
			hits++
			continue
		}
		pending = append(pending, pendingRead{
			key:           keyStr,
			entry:         outcome.entry,
			valueChan:     outcome.valueChan,
			needsSchedule: outcome.needsSchedule,
		})
	}
	return pending, hits, nil
}

// GetSizeInfo returns the current cache size (bytes) and entry count under the read lock.
func (s *shard) GetSizeInfo() (bytes uint64, entries uint64) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.cache.SizeInfoRLocked()
}

// IteratorOpened records that an iterator is reading this shard. Balanced by exactly one
// IteratorClosed.
func (s *shard) IteratorOpened() {
	s.lock.Lock()
	s.openIterators++
	s.lock.Unlock()
}

// IteratorClosed records that an iterator reading this shard has been closed. Closing more than were
// opened is refused rather than wrapping the count, which would make the leak report at Close useless.
func (s *shard) IteratorClosed() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.openIterators == 0 {
		return errors.New("an iterator was closed on a shard that has none open")
	}
	s.openIterators--
	return nil
}

// Set sets the value for the given key at the current version.
//
// A write to a shard that is out of service is refused: it would land in versioned data that no
// lifecycle runner remains to flush, and so be discarded silently.
func (s *shard) Set(key []byte, value []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return fmt.Errorf("set key %x: %w", key, err)
	}
	s.setWLocked(string(key), value)
	return nil
}

// setWLocked writes a value to the versioned data structures at the current version.
//
// key may be carved from a shared buffer: nothing retains it past this version's retirement without
// copying it first, so a caller allocating its keys out of one arena does not pin that arena for the
// life of the shard. value gets no such treatment — it is retained as given.
func (s *shard) setWLocked(key string, value []byte) {
	// Dropped whole when this version retires, so it can hold the caller's string as given.
	s.versionDiffs[s.currentVersion][key] = value

	deque, ok := s.versionedData[key]
	if !ok {
		deque = structures.NewDequeWithCapacity[versionedValue](s.initialVersionsPerKey)
		// Copied, because this map's entries outlive the version that created them and a Go string
		// can be a window onto a much larger allocation: a caller that cut its keys from one shared
		// buffer would pin that whole buffer here. Only on first insert — Go keeps a map's existing
		// key on reassignment, so copying later would have no effect.
		s.versionedData[strings.Clone(key)] = deque
	}
	if deque.IsEmpty() || deque.PeekBack().version < s.currentVersion {
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	} else {
		deque.PopBack()
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	}
}

// batchSetAt applies the writes named by indices, which index into writes. Refused on a shard that
// is out of service, for the reason given on Set.
func (s *shard) batchSetAt(writes []Write, indices []int) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return fmt.Errorf("batch set of %d keys: %w", len(indices), err)
	}
	for _, i := range indices {
		s.setWLocked(writes[i].Key, writes[i].Value)
	}
	return nil
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) error {
	return s.Set(key, nil)
}

// Commit seals the current version, so that all future updates apply to the next one, and runs the
// read cache's once-per-block maintenance. It returns the version the shard is now on, which the
// caller checks against its own, alongside any error.
//
// A shard that is out of service seals nothing and reports why, leaving its version unchanged. The
// check sits under the seal's own lock because a check that releases the lock first is one a shard
// can fall out of service behind.
func (s *shard) Commit() (uint64, error) {
	s.lock.Lock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		s.lock.Unlock()
		return s.currentVersion, fmt.Errorf("seal version %d: %w", s.currentVersion, err)
	}

	sealedVersion := s.currentVersion
	newVersion := s.currentVersion + 1
	s.currentVersion = newVersion

	// Registered at the seal rather than at the materialize, so that a consumer asking for a sealed
	// version's diff always finds a handle to wait on, however far ahead of it they arrive.
	s.sealedDiffs[sealedVersion] = &sealedDiff{done: make(chan struct{})}
	// Sized from the version just sealed, so a block's writes land in one allocation instead of
	// growing the map up from empty. Doubled, so a block that writes somewhat more than the last one
	// still does not resize.
	s.versionDiffs[newVersion] = make(map[string][]byte, 2*len(s.versionDiffs[sealedVersion]))

	// Sealing a version is the once-per-block moment the read cache does its eviction, so that no read
	// has to pay for it.
	err := s.cache.MaintainWLocked()

	s.lock.Unlock()

	if err != nil {
		return newVersion, fmt.Errorf("maintain the read cache after sealing version %d: %w",
			newVersion, err)
	}
	return newVersion, nil
}

// MaterializeSortedDiff turns a sealed version's writes into entries ordered by key, replacing the map
// they were accumulated in, and publishes them to everything waiting on that version. It returns once
// they are published, and does nothing for a version already materialized.
//
// This is the single place a sealed diff is produced: the flush, hashing, and retirement all read the
// result rather than the map.
func (s *shard) MaterializeSortedDiff(version uint64) error {
	handle, diff, err := s.claimDiffToMaterialize(version)
	if err != nil {
		return err
	}
	if diff == nil {
		// Claimed by another caller. Waited on rather than returned from, so that this reports what it
		// promises: when it returns, the version's diff is published, whoever ordered it.
		_, err := s.SortedDiff(version)
		return err
	}

	// Ordered outside the lock. The map is private to this call once claimed — it has left
	// versionDiffs, and writes land only at the current version — so the only work the lock covers is
	// publishing the result.
	entries := make([]Write, 0, len(diff))
	for key, value := range diff {
		entries = append(entries, Write{Key: key, Value: value})
	}
	// Ordering is what makes the diff cheap for pebble to absorb: its memtable is a skiplist that caches
	// the splice it last inserted at, which map order defeated. Bytewise, to match pebble's default
	// comparer, since that is what decides whether it ascends as far as the memtable is concerned.
	slices.SortFunc(entries, func(a Write, b Write) int {
		return strings.Compare(a.Key, b.Key)
	})

	s.lock.Lock()
	handle.entries = entries
	s.lock.Unlock()
	close(handle.done)

	return nil
}

// claimDiffToMaterialize takes a sealed version's diff map out of the shard for the caller to order,
// returning the handle to publish the result on. A nil map means the version needs no freezing because
// another caller has already claimed it.
func (s *shard) claimDiffToMaterialize(version uint64) (*sealedDiff, map[string][]byte, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if version >= s.currentVersion {
		return nil, nil, fmt.Errorf("version (%d) must be less than the current version (%d) to be "+
			"materialized", version, s.currentVersion)
	}
	handle, sealed := s.sealedDiffs[version]
	if !sealed {
		return nil, nil, fmt.Errorf("version (%d) is no longer tracked and cannot be materialized", version)
	}

	diff, unmaterialized := s.versionDiffs[version]
	if !unmaterialized {
		return handle, nil, nil
	}
	delete(s.versionDiffs, version)
	return handle, diff, nil
}

// SortedDiff returns a sealed version's writes ordered by key, waiting for them to be materialized if
// that has not happened yet. The returned entries must not be mutated, but are otherwise thread safe to read.
func (s *shard) SortedDiff(version uint64) ([]Write, error) {
	s.lock.RLock()
	handle, sealed := s.sealedDiffs[version]
	s.lock.RUnlock()
	if !sealed {
		return nil, fmt.Errorf("version (%d) is not a sealed version of this shard", version)
	}

	select {
	case <-handle.done:
	case <-s.ctx.Done():
		return nil, fmt.Errorf("view manager shut down while awaiting the diff at version %d: %w",
			version, s.shutdownError())
	}

	s.lock.RLock()
	defer s.lock.RUnlock()
	return handle.entries, nil
}

// MaterializeCurrentOverrides returns the in-memory overrides in this shard at the current version
// whose keys fall within [lowerBound, upperBound). A nil bound is unbounded on that side. The result
// is unsorted.
//
// Because the target is always the current version, each key resolves to the back of its deque —
// no version scan is needed, unlike lookupVersionedRLocked, which serves reads at older versions.
func (s *shard) MaterializeCurrentOverrides(lowerBound []byte, upperBound []byte) ([]kvPair, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Same reason the read paths check it: a shard taken out of service cannot vouch for its data,
	// and an iterator is just a bulk read.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, fmt.Errorf("materialize the current overrides: read the current overrides: %w", err)
	}

	out := make([]kvPair, 0, len(s.versionedData))
	for key, deque := range s.versionedData {
		if deque.IsEmpty() {
			continue
		}
		if lowerBound != nil && key < string(lowerBound) {
			continue
		}
		if upperBound != nil && key >= string(upperBound) {
			continue
		}
		newest := deque.PeekBack()
		out = append(out, kvPair{
			key:   []byte(key),
			value: newest.value,
		})
	}
	return out, nil
}

// Drop versions, pushing their data down into the read cache. The first version to drop must be
// equal to the oldest version currently being tracked.
func (s *shard) DropVersions(
	// The first version to drop (inclusive).
	firstVersion uint64,
	// The last version to drop (exclusive).
	lastVersion uint64,
) error {

	if firstVersion >= lastVersion {
		return fmt.Errorf("firstVersion (%d) must be less than lastVersion (%d)",
			firstVersion, lastVersion)
	}

	// Retirement reads the diffs, so it materializes the versions it is about to drop rather than
	// trusting that a flush already did. An already materialized version makes this a no-op, which is
	// the production case.
	for version := firstVersion; version < lastVersion; version++ {
		if err := s.MaterializeSortedDiff(version); err != nil {
			return fmt.Errorf("materialize version %d before retiring it: %w", version, err)
		}
	}

	diffs, err := s.claimVersionsToRetire(firstVersion, lastVersion)
	if err != nil {
		return err
	}

	// Moved in chunks, each under a short write lock, so that readers of this shard never wait out a
	// whole version. Every key's trim and cache insert land in one chunk, and older versions go first,
	// trimming only up to their own version, so a reader between chunks finds every key either still
	// in versionedData or already in the read cache.
	version := firstVersion
	for _, diff := range diffs {
		version++
		for start := 0; start < len(diff); start += retireChunkSize {
			chunk := diff[start:min(start+retireChunkSize, len(diff))]
			if err := s.retireChunk(chunk, version); err != nil {
				return err
			}
		}
	}
	return nil
}

// retireChunkSize is the number of keys DropVersions moves into the read cache per lock acquisition.
const retireChunkSize = 256

// claimVersionsToRetire takes the sealed diffs of [firstVersion, lastVersion) out of the shard, oldest
// first, and advances oldestVersion past them.
func (s *shard) claimVersionsToRetire(firstVersion uint64, lastVersion uint64) ([][]Write, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if firstVersion != s.oldestVersion {
		return nil, fmt.Errorf("firstVersion (%d) must be equal to the oldest version (%d)",
			firstVersion, s.oldestVersion)
	}
	if lastVersion > s.currentVersion {
		return nil, fmt.Errorf("lastVersion (%d) must be less than or equal to the current version (%d)",
			lastVersion, s.currentVersion)
	}

	diffs := make([][]Write, 0, lastVersion-firstVersion)
	for version := firstVersion; version < lastVersion; version++ {
		handle, sealed := s.sealedDiffs[version]
		if !sealed {
			return nil, fmt.Errorf("version %d is not a sealed version and cannot be retired", version)
		}
		select {
		case <-handle.done:
		default:
			// The materialize in DropVersions covers this, so reaching it means the lifecycle is broken.
			// Carrying on would retire keys whose values never reached the read cache: an unmaterialized
			// version's diff is empty, and an empty diff is indistinguishable from a version that wrote
			// nothing.
			return nil, fmt.Errorf("version %d was not materialized and cannot be retired", version)
		}
		diffs = append(diffs, handle.entries)
		delete(s.sealedDiffs, version)
	}

	// Advanced before the keys move: no view holds a retiring version, and versionedData still answers
	// for every key not yet moved.
	s.oldestVersion = lastVersion
	return diffs, nil
}

// retireChunk drops the values of chunk's keys older than retiredThrough from versionedData and
// installs chunk in the read cache, under one write lock.
func (s *shard) retireChunk(chunk []Write, retiredThrough uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, entry := range chunk {
		deque, tracked := s.versionedData[entry.Key]
		if !tracked {
			continue
		}
		for !deque.IsEmpty() && deque.PeekFront().version < retiredThrough {
			deque.PopFront()
		}
		if deque.IsEmpty() {
			delete(s.versionedData, entry.Key)
		}
	}
	return s.cache.PutRetiredWLocked([][]Write{chunk})
}

// TakeOutOfService stops this shard from serving reads and accepting writes, reporting err as the
// cause. Called on every shard when the manager shuts down, so a failure anywhere stops every shard.
func (s *shard) TakeOutOfService(err error) {
	s.lock.Lock()
	s.cache.TakeOutOfServiceWLocked(err)
	s.lock.Unlock()
}
