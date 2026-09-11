package view

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

	// For each version, contains the values set in that version.  If a value is set more than once
	// in a version, only the last value is stored. Although possible to find the value at a specifc
	// version by iterating over this map, it is much more efficient to use the versionedData map.
	versionDiffs map[uint64] /* version */ map[string] /* key */ []byte /* value */

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
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,
		versionLatches: make(map[uint64]*versionLatch),
		ctx:            ctx,
		shutdownError:  shutdownError,

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
	if entry, found := s.lookupVersionedRLocked(string(key), version); found {
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

	if entry, found := s.lookupVersionedRLocked(string(key), version); found {
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
func (s *shard) lookupVersionedRLocked(key string, version uint64) (versionedValue, bool) {
	deque, ok := s.versionedData[key]
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
		if entry, found := s.lookupVersionedRLocked(keyStr, version); found {
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
		if entry, found := s.lookupVersionedRLocked(keyStr, version); found {
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
	s.setWLocked(key, value)
	return nil
}

// setWLocked writes a value to the versioned data structures at the current version.
func (s *shard) setWLocked(key []byte, value []byte) {
	keyStr := string(key)
	s.versionDiffs[s.currentVersion][keyStr] = value

	deque, ok := s.versionedData[keyStr]
	if !ok {
		deque = structures.NewDeque[versionedValue]()
		s.versionedData[keyStr] = deque
	}
	if deque.IsEmpty() || deque.PeekBack().version < s.currentVersion {
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	} else {
		deque.PopBack()
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	}
}

// BatchSet sets the values for a batch of keys at the current version. Refused on a shard that is
// out of service, for the reason given on Set.
func (s *shard) BatchSet(entries []*proto.KVPair) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return fmt.Errorf("batch set of %d keys: %w", len(entries), err)
	}
	for i := range entries {
		if entries[i].Delete {
			// A delete is stored as a nil-valued (tombstone) entry at the current version.
			s.setWLocked(entries[i].Key, nil)
		} else {
			s.setWLocked(entries[i].Key, entries[i].Value)
		}
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
		deque = structures.NewDeque[versionedValue]()
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

// Commit seals the current version; all future updates will be applied to the next version. It also
// runs the read cache's once-per-block maintenance, whose failure it returns.
// The version returned is the new version number (for sanity checking), and is returned even alongside
// an error so the caller can report both.
func (s *shard) Commit() (uint64, error) {
	s.lock.Lock()

	newVersion := s.currentVersion + 1
	s.currentVersion = newVersion

	s.versionDiffs[newVersion] = make(map[string][]byte)

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

// Get the diffs for a range of versions [firstVersion, lastVersion). The returned data should not be mutated
// in any way, but is otherwise thread safe to read.
func (s *shard) GetDiffsForVersions(
	// The first version to include (inclusive).
	firstVersion uint64,
	// The last version to include (exclusive).
	lastVersion uint64,
) ([]map[string][]byte, error) {

	if firstVersion > lastVersion {
		return nil, fmt.Errorf("firstVersion (%d) must be less than or equal to lastVersion (%d)",
			firstVersion, lastVersion)
	}

	// Awaited before the lock is taken, not after: a sealed version's diff keeps being written to
	// while its folds resolve, so it is frozen only once its latch has opened. This is the single
	// place the diff consumers — hashing, flushing, and the retirement that follows a flush — reach
	// a version's values, which is why the wait belongs here rather than at each of them.
	for version := firstVersion; version < lastVersion; version++ {
		if err := s.awaitVersionFoldsUnlocked(version); err != nil {
			return nil, fmt.Errorf("await version %d before reading its diff: %w", version, err)
		}
	}

	// A read lock suffices, and it matters: sort jobs for different versions call this concurrently.
	// Nothing here mutates the shard, and the maps handed back are frozen — a version whose latch has
	// opened gains no further writes.
	s.lock.RLock()
	defer s.lock.RUnlock()

	if firstVersion < s.oldestVersion {
		return nil, fmt.Errorf("firstVersion (%d) must be greater than or equal to the oldest version (%d)",
			firstVersion, s.oldestVersion)
	}
	if lastVersion > s.currentVersion {
		return nil, fmt.Errorf("lastVersion (%d) must be less than or equal to the current version (%d)",
			lastVersion, s.currentVersion)
	}

	diffs := make([]map[string][]byte, 0, lastVersion-firstVersion)
	for v := firstVersion; v < lastVersion; v++ {
		diffs = append(diffs, s.versionDiffs[v])
	}
	return diffs, nil
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

	// Combine the data from all versions being dropped.
	var combinedData map[string][]byte
	if firstVersion == lastVersion-1 {
		// single version
		combinedData = s.versionDiffs[firstVersion]
	} else {
		// range of versions
		combinedData = make(map[string][]byte)
		for version := firstVersion; version < lastVersion; version++ {
			for k, value := range s.versionDiffs[version] {
				combinedData[k] = value
			}
		}
	}

	// Drop the version diffs that we will no longer need.
	for v := firstVersion; v < lastVersion; v++ {
		delete(s.versionDiffs, v)
	}

	// Clean up the versioned data map.
	for k := range combinedData {
		deque := s.versionedData[k]
		for !deque.IsEmpty() {
			next := deque.PeekFront()
			if next.version >= lastVersion {
				break
			}
			deque.PopFront()
		}
		if deque.IsEmpty() {
			delete(s.versionedData, k)
		}
	}

	// Push the combined data down into the read cache, still under the same lock grab, so
	// readers never observe an intermediate state between the deque cleanup and the cache
	// insert.
	retireErr := s.cache.PutRetiredWLocked(combinedData)

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
