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
	// whose folds are still resolving. A key set more than once in a version keeps only its last value.
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

	// ViewManager-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *ViewManagerMetrics

	// The current version number.
	currentVersion uint64

	// The oldest version number kept in versionedData.
	oldestVersion uint64

	// The number of iterators currently reading this shard. Close reports a non-zero count as a
	// leaked iterator, since reading one after the database has closed is undefined behaviour (see
	// ViewManager.Close).
	openIterators uint64

	// versionLatches holds a latch for each version that still has staged folds outstanding; a
	// version absent from the map has none. Guarded by the shard lock. See versionLatch.
	versionLatches map[uint64]*versionLatch

	// ctx is cancelled when the manager shuts down. Awaits on a staged value observe it, because a
	// fold interrupted by shutdown never resolves.
	ctx context.Context

	// shutdownError names the cause once ctx is cancelled.
	shutdownError func() error

	// reportFoldFailure bricks the manager. A fold that cannot complete leaves a version unhashable,
	// so it has to stop the whole manager rather than only this shard — the same response the read
	// cache gives a failed database read.
	reportFoldFailure func(error)

	// initialVersionsPerKey sizes the value list a key gets when first written. See
	// ViewManagerConfig.InitialVersionsPerKey.
	initialVersionsPerKey int
}

// Write is one key's change: a value to store, or a deletion.
//
// A nil Value is a deletion. That is the manager's tombstone convention throughout — BatchUpdater
// returns nil from NewValueFor to delete, and the version diff maps hold nil for a deleted key — so a
// caller with a genuinely empty value passes a non-nil, zero-length slice.
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
	// pending is non-nil while this value's bytes are still being folded. Every path that reads value
	// must check it first.
	pending *pendingValue
}

// versionLatch counts a version's unresolved staged values and lets an observer wait for the last of
// them. A latch that has opened stays open, and one left incomplete by a failure carries that failure.
type versionLatch struct {
	// How many of this version's staged values have not resolved. Guarded by the shard lock.
	count int

	// Closed when count reaches zero.
	done chan struct{}

	// The fold failure that left this version incomplete, if one did.
	err error
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
	// Reports a fold that could not produce its value to the manager, which bricks. Distinct from
	// reportReadFailure so the latched error names the failure that actually happened.
	reportFoldFailure func(error),
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
	if reportFoldFailure == nil {
		// A fold failure leaves a version's diff incomplete. A shard that cannot report one would let
		// that version be hashed as though it were whole.
		return nil, fmt.Errorf("reportFoldFailure must be non-nil")
	}

	versionDiffs := make(map[uint64]map[string][]byte)
	versionDiffs[1] = make(map[string][]byte) // versions start at 1

	s := &shard{
		versionedData:  make(map[string]*structures.Deque[versionedValue]),
		versionDiffs:   versionDiffs,
		sealedDiffs:    make(map[uint64]*sealedDiff),
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,
		versionLatches: make(map[uint64]*versionLatch),
		ctx:            ctx,
		shutdownError:  shutdownError,

		initialVersionsPerKey: int(config.InitialVersionsPerKey), //nolint:gosec // validated non-zero

		reportFoldFailure: reportFoldFailure,
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
	value, found, pending, done, err := s.attemptFastGetUnlocked(key, version, updateLru)
	if pending != nil {
		value, err := pending.await(s.ctx, s.shutdownError)
		if err != nil {
			return nil, false, fmt.Errorf("get key %x at version %d: %w", key, version, err)
		}
		return value, value != nil, nil
	}
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
		if entry.pending != nil {
			value, err := entry.pending.await(s.ctx, s.shutdownError)
			if err != nil {
				return nil, false, fmt.Errorf("get key %x at version %d: %w", key, version, err)
			}
			return value, value != nil, nil
		}
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
//
// A key holding an unresolved fold is reported as pending, for the caller to await once it has
// released the lock.
func (s *shard) attemptFastGetUnlocked(
	key []byte,
	version uint64,
	updateLru bool,
) (value []byte, found bool, pending *pendingValue, done bool, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, false, nil, true, fmt.Errorf("key %x: %w", key, err)
	}
	if err := s.validateVersionRLocked(version); err != nil {
		return nil, false, nil, true, fmt.Errorf("key %x: %w", key, err)
	}

	if entry, found := s.lookupVersionedRLocked(key, version); found {
		if entry.pending != nil {
			return nil, false, entry.pending, false, nil
		}
		s.metrics.reportCacheHits(1)
		return entry.value, entry.value != nil, nil, true, nil
	}

	value, found, ok := s.cache.AttemptFastLookupRLocked(key, updateLru)
	if !ok {
		return nil, false, nil, false, nil
	}
	s.metrics.reportCacheHits(1)
	return value, found, nil, true, nil
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
//
// The entry may be an unresolved fold, so every caller has to check its pending field before reading
// its value. Resolving one requires releasing this lock first; see pendingValue.await.
func (s *shard) lookupVersionedRLocked(key []byte, version uint64) (versionedValue, bool) {
	// Converted inline rather than by the caller: the compiler elides the conversion only where it
	// indexes a map directly, and every single-key read pays an allocation for it otherwise.
	deque, ok := s.versionedData[string(key)]
	if !ok {
		return versionedValue{}, false
	}
	if version == s.oldestVersion {
		next := deque.PeekFront()
		if next.version == version {
			return next, true
		}
		return versionedValue{}, false
	}
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

	unresolved, staged, hits, err := s.attemptFastBatchGetUnlocked(keys, results, version)
	if err != nil {
		return nil, fmt.Errorf("batch get of %d keys at version %d: %w", len(keys), version, err)
	}

	var pending []pendingRead
	if len(unresolved) > 0 {
		var remainingHits int64
		var remainingStaged []*pendingValue
		pending, remainingStaged, remainingHits, err =
			s.batchGetRemainingUnlocked(keys, unresolved, results, version)
		if err != nil {
			return nil, fmt.Errorf("batch get of %d unresolved keys at version %d: %w",
				len(unresolved), version, err)
		}
		hits += remainingHits
		staged = append(staged, remainingStaged...)
	}

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	if err := s.cache.ResolveBatchUnlocked(pending, results); err != nil {
		// DB errors are fatal; fail the whole batch.
		return nil, fmt.Errorf("complete %d database reads for a batch get: %w", len(pending), err)
	}
	// Awaited after the DB reads and outside every lock, for the reason given on pendingValue.await.
	if err := s.awaitStagedReadsUnlocked(staged, results); err != nil {
		return nil, fmt.Errorf("batch get at version %d: %w", version, err)
	}
	return results, nil
}

// awaitStagedReadsUnlocked completes the staged folds a batch read ran into, writing each resolved value into
// results. A deleted key resolves to nil and is left out, as it would be on any other read path.
func (s *shard) awaitStagedReadsUnlocked(staged []*pendingValue, results map[string][]byte) error {
	for _, pending := range staged {
		value, err := pending.await(s.ctx, s.shutdownError)
		if err != nil {
			return fmt.Errorf("await staged value for key %x: %w", pending.key, err)
		}
		if value != nil {
			results[pending.key] = value
		}
	}
	return nil
}

// attemptFastBatchGetUnlocked resolves the keys it can while holding the read lock, writing found
// values into results and returning the positions in keys of those it could not resolve.
func (s *shard) attemptFastBatchGetUnlocked(
	keys [][]byte,
	results map[string][]byte,
	version uint64,
) (unresolved []int, staged []*pendingValue, hits int64, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, nil, 0, fmt.Errorf("resolve what is already in memory: %w", err)
	}

	if err := s.validateVersionRLocked(version); err != nil {
		return nil, nil, 0, fmt.Errorf("resolve what is already in memory: %w", err)
	}

	for i, key := range keys {
		keyStr := string(key)
		if entry, found := s.lookupVersionedRLocked(key, version); found {
			if entry.pending != nil {
				staged = append(staged, entry.pending)
				continue
			}
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
	return unresolved, staged, hits, nil
}

// batchGetRemainingUnlocked classifies the keys at the given positions in keys, which are those the
// fast pass could not resolve, creating entries and scheduling DB reads as needed.
func (s *shard) batchGetRemainingUnlocked(
	keys [][]byte,
	indices []int,
	results map[string][]byte,
	version uint64,
) (pending []pendingRead, staged []*pendingValue, hits int64, err error) {
	pending = make([]pendingRead, 0, len(indices))

	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, nil, 0, fmt.Errorf("classify the remaining keys: %w", err)
	}

	if err := s.validateVersionRLocked(version); err != nil {
		return nil, nil, 0, fmt.Errorf("classify the remaining keys: %w", err)
	}

	// Redone from scratch rather than carried over from the fast pass, because the lock was released
	// in between and another reader may have scheduled or completed any of these keys — or staged a
	// fold for one.
	for _, i := range indices {
		key := keys[i]
		keyStr := string(key)
		if entry, found := s.lookupVersionedRLocked(key, version); found {
			if entry.pending != nil {
				staged = append(staged, entry.pending)
				continue
			}
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
	return pending, staged, hits, nil
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

// StageUpdates reserves a slot at the current version for each key named by indices, each holding an
// unresolved fold, and reports where each of those folds gets the value it folds onto. Folds staged
// for one key apply in the order they were staged.
func (s *shard) StageUpdates(
	keys []string,
	indices []int,
	version uint64,
) ([]stagedFold, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, fmt.Errorf("stage %d values at version %d: %w", len(indices), version, err)
	}
	if version != s.currentVersion {
		return nil, fmt.Errorf("staging at version %d, but the current version is %d",
			version, s.currentVersion)
	}

	folds := make([]stagedFold, len(indices))
	for n, index := range indices {
		key := keys[index]
		folds[n] = stagedFold{
			prior:  s.capturePriorValueWLocked(key),
			result: newPendingValue(key),
		}
		s.stagePendingValueWLocked(key, version, folds[n].result)
	}
	return folds, nil
}

// capturePriorValueWLocked reports what a fold staged now for key would be folding on top of: the newest
// value the shard holds, resolved or not, or neither when the shard holds none and it has to come
// from the read cache or the database.
func (s *shard) capturePriorValueWLocked(key string) priorValueSource {
	deque, ok := s.versionedData[key]
	if !ok || deque.IsEmpty() {
		return priorValueSource{location: priorValueInReadCache}
	}
	// The newest entry is the right one to fold onto, whether it belongs to an earlier version or to
	// an earlier write within this one. It can never belong to a later version: every write lands at
	// the current version, and StageUpdates refuses any other, so nothing is ever appended above it.
	newest := deque.PeekBack()
	if newest.pending != nil {
		return priorValueSource{location: priorValueInEarlierFold, pending: newest.pending}
	}
	return priorValueSource{location: priorValueInVersionedData, value: newest.value}
}

// stagePendingValueWLocked puts an unresolved fold into the versioned data at the given version,
// replacing any entry this version already had for the key.
func (s *shard) stagePendingValueWLocked(key string, version uint64, pending *pendingValue) {
	entry := versionedValue{version: version, pending: pending}

	deque, ok := s.versionedData[key]
	if !ok {
		deque = structures.NewDequeWithCapacity[versionedValue](s.initialVersionsPerKey)
		// Cloned because this map entry outlives the batch that created it, and Go leaves a map's
		// original key in place on reassignment. The copy is per key new to this shard, not per write.
		s.versionedData[strings.Clone(key)] = deque
	}
	if deque.IsEmpty() || deque.PeekBack().version < version {
		deque.PushBack(entry)
	} else {
		deque.PopBack()
		deque.PushBack(entry)
	}

	s.markFoldStagedWLocked(version)
}

// markFoldStagedWLocked records one more of a version's folds as outstanding, creating the version's
// latch if this is its first.
func (s *shard) markFoldStagedWLocked(version uint64) {
	latch, ok := s.versionLatches[version]
	if !ok {
		latch = &versionLatch{done: make(chan struct{})}
		s.versionLatches[version] = latch
	}
	latch.count++
}

// markFoldResolvedWLocked records one of a version's folds as no longer outstanding, opening the
// version's latch when it was the last. A version left incomplete by a failure keeps its latch.
func (s *shard) markFoldResolvedWLocked(version uint64) {
	latch, ok := s.versionLatches[version]
	if !ok {
		return
	}
	latch.count--
	if latch.count > 0 {
		return
	}
	close(latch.done)
	if latch.err == nil {
		delete(s.versionLatches, version)
	}
}

// awaitVersionFoldsUnlocked blocks until every fold staged in the given version has resolved, reporting
// the failure that stopped one if any did. Must be called with no lock held.
func (s *shard) awaitVersionFoldsUnlocked(version uint64) error {
	s.lock.RLock()
	latch, outstanding := s.versionLatches[version]
	s.lock.RUnlock()
	if !outstanding {
		return nil
	}

	select {
	case <-latch.done:
	case <-s.ctx.Done():
		return fmt.Errorf("view manager shut down while awaiting version %d: %w",
			version, s.shutdownError())
	}

	s.lock.RLock()
	defer s.lock.RUnlock()
	if latch.err != nil {
		return fmt.Errorf("version %d holds a value that failed to resolve: %w", version, latch.err)
	}
	return nil
}

// AwaitOutstandingFolds blocks until every fold this shard has staged has resolved, whatever it
// resolved to.
//
// The wait is not interruptible: it exists to keep a shutdown from overtaking a fold, and a manager
// that has already failed has already cancelled the context an interruptible wait would observe.
// Every fold resolves its version's latch whether it produced a value or failed, so the wait
// terminates either way.
func (s *shard) AwaitOutstandingFolds() {
	s.lock.RLock()
	latches := make([]*versionLatch, 0, len(s.versionLatches))
	for _, latch := range s.versionLatches {
		latches = append(latches, latch)
	}
	s.lock.RUnlock()

	for _, latch := range latches {
		<-latch.done
	}
}

// FoldStagedValues folds every value a batch staged and records what each produced. Either every fold
// in the batch is recorded or none is.
func (s *shard) FoldStagedValues(folds []stagedFold, updater BatchUpdater, version uint64) {
	priorValues, err := s.resolvePriorValuesUnlocked(folds)
	if err != nil {
		s.FailStagedFolds(folds, version, err)
		return
	}

	newValues := make([][]byte, len(folds))
	for n := range folds {
		newValues[n], err = updater.NewValueFor(folds[n].result.key, priorValues[n])
		if err != nil {
			s.FailStagedFolds(folds, version,
				fmt.Errorf("fold key %x at version %d: %w", folds[n].result.key, version, err))
			return
		}
	}

	s.recordFoldsUnlocked(folds, newValues, version)
}

// resolvePriorValuesUnlocked produces the value each staged fold applies on top of.
func (s *shard) resolvePriorValuesUnlocked(folds []stagedFold) ([][]byte, error) {
	values := make([][]byte, len(folds))
	var needRead []int

	for n := range folds {
		switch folds[n].prior.location {
		case priorValueInVersionedData:
			values[n] = folds[n].prior.value
		case priorValueInEarlierFold:
			// Awaited outside the lock, which is why this runs here rather than during staging.
			value, err := folds[n].prior.pending.await(s.ctx, s.shutdownError)
			if err != nil {
				return nil, fmt.Errorf("await the earlier fold of key %x: %w", folds[n].result.key, err)
			}
			values[n] = value
		case priorValueInReadCache:
			needRead = append(needRead, n)
		default:
			// The zero value lands here, which is the point: a priorValueSource built with its
			// location left unset would otherwise be served as though its value had been read.
			panic(fmt.Sprintf("unexpected prior value location: %#v", folds[n].prior.location))
		}
	}
	if len(needRead) == 0 {
		return values, nil
	}

	// Read through the cache rather than the versioned lookup: versioned data already holds this
	// batch's own unresolved entry for these keys, so a versioned lookup would find each fold waiting
	// on itself.
	results := make(map[string][]byte, len(needRead))
	unresolved, err := s.priorValuesFromCacheUnlocked(folds, needRead, results)
	if err != nil {
		return nil, fmt.Errorf("read %d prior values from the cache: %w", len(needRead), err)
	}
	if len(unresolved) > 0 {
		pending, err := s.schedulePriorValueReadsUnlocked(folds, unresolved, results)
		if err != nil {
			return nil, fmt.Errorf("schedule %d prior value reads: %w", len(unresolved), err)
		}
		if err := s.cache.ResolveBatchUnlocked(pending, results); err != nil {
			return nil, fmt.Errorf("complete %d prior value reads: %w", len(pending), err)
		}
	}

	for _, n := range needRead {
		values[n] = results[folds[n].result.key]
	}
	return values, nil
}

// priorValuesFromCacheUnlocked resolves the prior values the cache already holds, returning the
// positions it could not.
func (s *shard) priorValuesFromCacheUnlocked(
	folds []stagedFold,
	positions []int,
	results map[string][]byte,
) ([]int, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, fmt.Errorf("look up %d prior values: %w", len(positions), err)
	}

	var unresolved []int
	for _, n := range positions {
		key := folds[n].result.key
		value, found, ok := s.cache.AttemptFastLookupRLocked([]byte(key), false)
		if !ok {
			unresolved = append(unresolved, n)
			continue
		}
		if found {
			results[key] = value
		}
	}
	return unresolved, nil
}

// schedulePriorValueReadsUnlocked classifies the prior values the cache could not resolve, scheduling
// database reads as needed. The reads themselves are completed by the caller, outside the lock.
func (s *shard) schedulePriorValueReadsUnlocked(
	folds []stagedFold,
	positions []int,
	results map[string][]byte,
) ([]pendingRead, error) {
	pending := make([]pendingRead, 0, len(positions))

	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, fmt.Errorf("classify %d prior value reads: %w", len(positions), err)
	}

	for _, n := range positions {
		key := folds[n].result.key
		outcome := s.cache.LookupWLocked([]byte(key), false)
		if outcome.immediate {
			if outcome.found {
				results[key] = outcome.value
			}
			continue
		}
		pending = append(pending, pendingRead{
			key:           key,
			entry:         outcome.entry,
			valueChan:     outcome.valueChan,
			needsSchedule: outcome.needsSchedule,
		})
	}
	return pending, nil
}

// recordFoldsUnlocked stores what every fold in a batch produced: into the versioned entry staged for it,
// into the version's diff, and into the handle its observers are waiting on.
func (s *shard) recordFoldsUnlocked(folds []stagedFold, newValues [][]byte, version uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for n := range folds {
		key := folds[n].result.key
		if s.fillStagedValueWLocked(key, version, folds[n].result, newValues[n]) {
			s.versionDiffs[version][key] = newValues[n]
		}
		s.markFoldResolvedWLocked(version)
		// Released under the lock deliberately. A woken observer reads the versioned data, so it has
		// to queue behind this hold anyway, and releasing here leaves no window in which the entry is
		// filled but its observers are still parked.
		folds[n].result.inject(newValues[n], nil)
	}
}

// fillStagedValueWLocked replaces a staged entry with the value its fold produced, reporting whether
// that value is still the one the key holds at this version. A value that is not must be kept out of
// the version's diff.
func (s *shard) fillStagedValueWLocked(
	key string,
	version uint64,
	pending *pendingValue,
	value []byte,
) bool {
	deque, ok := s.versionedData[key]
	if !ok {
		// Retirement is the only thing that removes a key, and it refuses a version whose folds are
		// still outstanding. Reaching here means that invariant broke, and carrying on would lose
		// the write silently.
		panic(fmt.Sprintf("no versioned data for staged key %x at version %d", key, version))
	}

	for i := deque.Len() - 1; i >= 0; i-- {
		entry := deque.Get(i)
		if entry.pending == pending {
			deque.Set(i, versionedValue{value: value, version: version})
			return true
		}
		if entry.version < version {
			break
		}
	}
	// The entry is gone, so a later fold or plain write at this version took its place and won. At most
	// one entry per version exists, so finding none means this fold's value has been superseded.
	return false
}

// FailStagedFolds records a failed fold on every value a batch staged and takes the shard out of
// service. The version keeps its latch, carrying the failure.
func (s *shard) FailStagedFolds(folds []stagedFold, version uint64, err error) {
	s.lock.Lock()
	if latch, ok := s.versionLatches[version]; ok && latch.err == nil {
		latch.err = err
	}
	s.cache.TakeOutOfServiceWLocked(err)
	for n := range folds {
		s.markFoldResolvedWLocked(version)
		folds[n].result.inject(nil, err)
	}
	s.lock.Unlock()

	// Reported after the lock is released: bricking takes the manager's versionLock and then every
	// shard's, this one included.
	s.reportFoldFailure(err)
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
	// A sealed version keeps being written to while its folds resolve, so it can be materialized only
	// once its latch has opened.
	if err := s.awaitVersionFoldsUnlocked(version); err != nil {
		return fmt.Errorf("await version %d before materializing its diff: %w", version, err)
	}

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
	// versionDiffs and the latch guarantees no writer remains — so the only work the lock covers is
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
	// Retried rather than resolved in place, because a fold cannot complete while this lock is held.
	// One retry is the normal case: iterator construction must not race a batch write, so no further
	// values are staged while the first pass's folds are awaited.
	for {
		pairs, staged, err := s.materializeAttemptUnlocked(lowerBound, upperBound)
		if err != nil {
			return nil, fmt.Errorf("materialize the current overrides: %w", err)
		}
		if len(staged) == 0 {
			return pairs, nil
		}
		for _, pending := range staged {
			if _, err := pending.await(s.ctx, s.shutdownError); err != nil {
				return nil, fmt.Errorf("await a staged value before materializing: %w", err)
			}
		}
	}
}

// materializeAttemptUnlocked copies the in-memory overrides in range, or reports the folds that have
// to resolve before they can be copied. A non-empty staged result means pairs is incomplete.
func (s *shard) materializeAttemptUnlocked(
	lowerBound []byte,
	upperBound []byte,
) (pairs []kvPair, staged []*pendingValue, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Same reason the read paths check it: a shard taken out of service cannot vouch for its data,
	// and an iterator is just a bulk read.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, nil, fmt.Errorf("read the current overrides: %w", err)
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
		if newest.pending != nil {
			staged = append(staged, newest.pending)
			continue
		}
		out = append(out, kvPair{
			key:   []byte(key),
			value: newest.value,
		})
	}
	if len(staged) > 0 {
		return nil, staged, nil
	}
	return out, nil, nil
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
	// the production case; it cannot happen under the lock below, because materializing waits for folds.
	for version := firstVersion; version < lastVersion; version++ {
		if err := s.MaterializeSortedDiff(version); err != nil {
			return fmt.Errorf("materialize version %d before retiring it: %w", version, err)
		}
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	if firstVersion != s.oldestVersion {
		return fmt.Errorf("firstVersion (%d) must be equal to the oldest version (%d)",
			firstVersion, s.oldestVersion)
	}
	if lastVersion > s.currentVersion {
		return fmt.Errorf("lastVersion (%d) must be less than or equal to the current version (%d)",
			lastVersion, s.currentVersion)
	}

	// Retirement is driven off the version diffs, so a version with folds outstanding would retire
	// without the keys those folds have yet to write: their versioned entries would never be dropped
	// and their values would never reach the cache. Retirement only ever follows a flush, which waits
	// for the same latch, so this reports a broken lifecycle rather than a race to be waited out.
	for version := firstVersion; version < lastVersion; version++ {
		if latch, outstanding := s.versionLatches[version]; outstanding {
			return fmt.Errorf("version %d still has %d unresolved value(s) and cannot be retired",
				version, latch.count)
		}
	}

	// Gather the diffs oldest version first. The cache insert below replays them in that order, so a
	// key written in several retiring versions ends up holding the newest of those values — which is
	// what combining them into one map used to do.
	diffs := make([][]Write, 0, lastVersion-firstVersion)
	for version := firstVersion; version < lastVersion; version++ {
		handle, sealed := s.sealedDiffs[version]
		if !sealed {
			return fmt.Errorf("version %d is not a sealed version and cannot be retired", version)
		}
		select {
		case <-handle.done:
		default:
			// The materialize above covers this, so reaching it means the lifecycle is broken. Carrying on
			// would retire keys whose values never reached the read cache: an unmaterialized version's diff
			// is empty, and an empty diff is indistinguishable from a version that wrote nothing.
			return fmt.Errorf("version %d was not materialized and cannot be retired", version)
		}
		diffs = append(diffs, handle.entries)
		delete(s.sealedDiffs, version)
	}

	// Clean up the versioned data map. A key written in more than one retiring version is visited
	// once per diff, and the trim is idempotent, so the repeat costs a lookup and finds nothing to do.
	for _, diff := range diffs {
		for _, entry := range diff {
			deque, tracked := s.versionedData[entry.Key]
			if !tracked {
				continue
			}
			for !deque.IsEmpty() {
				next := deque.PeekFront()
				if next.version >= lastVersion {
					break
				}
				deque.PopFront()
			}
			if deque.IsEmpty() {
				delete(s.versionedData, entry.Key)
			}
		}
	}

	// Push the retired data down into the read cache, still under the same lock grab, so
	// readers never observe an intermediate state between the deque cleanup and the cache
	// insert.
	retireErr := s.cache.PutRetiredWLocked(diffs)

	// Advanced even when the insert reported a failure: the keys have already moved into the cache, so
	// leaving oldestVersion behind would describe a migration that did not happen.
	s.oldestVersion = lastVersion

	return retireErr
}

// TakeOutOfService stops this shard from serving reads and accepting writes, reporting err as the
// cause. Called on every shard when the manager shuts down, so a failure anywhere stops every shard.
func (s *shard) TakeOutOfService(err error) {
	s.lock.Lock()
	s.cache.TakeOutOfServiceWLocked(err)
	s.lock.Unlock()
}
