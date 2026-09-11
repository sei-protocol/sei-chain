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
// Method postfixes state the lock contract: RLocked and WLocked require the caller to hold the read or
// write lock, Unlocked requires the caller to hold neither, and a bare name has no lock dependency.
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
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,
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
	if value, found, done, err := s.attemptFastGetUnlocked(key, version, updateLru); done {
		return value, found, err
	}

	// Not resolvable without mutating: classify against the DB read-cache under the write lock,
	// then complete the read (which may schedule a DB read and block) outside it. Redone from scratch
	// because the lock was released in between, so another reader may have scheduled this key.
	s.lock.Lock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		s.lock.Unlock()
		return nil, false, err
	}

	if err := s.validateVersionRLocked(version); err != nil {
		s.lock.Unlock()
		return nil, false, err
	}

	// First, check to see if we have this value in the versioned data map.
	if value, found := s.lookupVersionedRLocked(string(key), version); found {
		s.lock.Unlock()
		s.metrics.reportCacheHits(1)
		return value, value != nil, nil
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
		return nil, false, true, err
	}
	if err := s.validateVersionRLocked(version); err != nil {
		return nil, false, true, err
	}

	if value, found := s.lookupVersionedRLocked(string(key), version); found {
		s.metrics.reportCacheHits(1)
		return value, value != nil, true, nil
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

// lookupVersionedRLocked checks versioned data for a key at the given version.
// Returns (value, true) if found in versioned data, (nil, false) if the read cache should be
// consulted.
func (s *shard) lookupVersionedRLocked(key string, version uint64) ([]byte, bool) {
	deque, ok := s.versionedData[key]
	if !ok {
		return nil, false
	}
	if version == s.oldestVersion {
		next := deque.PeekFront()
		if next.version == version {
			return next.value, true
		}
		return nil, false
	}
	for i := deque.Len() - 1; i >= 0; i-- {
		next := deque.Get(i)
		if next.version <= version {
			return next.value, true
		}
	}
	return nil, false
}

// batchGet reads the keys named by indices at the given version, writing each key's value into
// values at that key's own index. A key this shard has no value for is left alone, so the caller's
// nil stands for not-found; a found value is never nil, which is what makes the two distinguishable.
//
// keys and values are the caller's full-batch slices, indexed alike, and only the elements named by
// indices are read or written. Concurrent calls for different shards are therefore safe: a key's
// shard is a function of the key, so no two shards share an index.
//
// Any read error fails the call, and the elements it did not reach keep whatever they held.
//
// A batch is classified in two passes so that a large one does not hold the shard exclusively for its
// whole length. That matters more here than for a single read: Go's RWMutex queues arriving readers
// behind a waiting writer, so one long exclusive hold stalls every read on this shard and convoys
// those that follow it. The first pass takes the shared lock and resolves everything already present;
// only keys it could not resolve reach the second, which needs the exclusive lock to create entries
// and schedule DB reads.
func (s *shard) batchGet(keys []string, indices []int, values [][]byte, version uint64) error {
	unresolved, hits, err := s.attemptFastBatchGetUnlocked(keys, indices, values, version)
	if err != nil {
		return err
	}

	var pending []pendingRead
	if len(unresolved) > 0 {
		pending, err = s.batchGetRemainingUnlocked(keys, unresolved, values, version, &hits)
		if err != nil {
			return err
		}
	}

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	// DB errors are fatal; they fail the whole batch.
	return s.cache.ResolveBatchUnlocked(pending, values)
}

// attemptFastBatchGetUnlocked resolves the keys it can under the read lock, returning the positions of those
// it could not.
func (s *shard) attemptFastBatchGetUnlocked(
	keys []string,
	indices []int,
	values [][]byte,
	version uint64,
) (unresolved []int, hits int64, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, 0, err
	}

	if err := s.validateVersionRLocked(version); err != nil {
		return nil, 0, err
	}

	for _, index := range indices {
		key := keys[index]
		if value, found := s.lookupVersionedRLocked(key, version); found {
			// found includes tombstones, whose nil value lands as the not-found the caller reads it as.
			values[index] = value
			hits++
			continue
		}

		// The batch path never records recency on hits, hence updateLru=false.
		value, _, ok := s.cache.AttemptFastLookupStringRLocked(key, false)
		if ok {
			// Resolved from cache. A deleted key carries a nil value, as above.
			values[index] = value
			hits++
			continue
		}
		unresolved = append(unresolved, index)
	}
	return unresolved, hits, nil
}

// batchGetRemainingUnlocked classifies the keys the fast pass could not, creating entries and
// scheduling DB reads as needed.
//
// The whole classification is redone for these keys rather than carried over, because the lock was
// released in between and another reader may have scheduled or completed any of them.
func (s *shard) batchGetRemainingUnlocked(
	keys []string,
	indices []int,
	values [][]byte,
	version uint64,
	hits *int64,
) ([]pendingRead, error) {
	pending := make([]pendingRead, 0, len(indices))

	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, err
	}

	if err := s.validateVersionRLocked(version); err != nil {
		return nil, err
	}

	for _, index := range indices {
		key := keys[index]
		if value, found := s.lookupVersionedRLocked(key, version); found {
			values[index] = value
			*hits++
			continue
		}

		outcome := s.cache.LookupStringWLocked(key, false)
		if outcome.immediate {
			values[index] = outcome.value
			*hits++
			continue
		}
		pending = append(pending, pendingRead{
			key:           key,
			index:         index,
			entry:         outcome.entry,
			valueChan:     outcome.valueChan,
			needsSchedule: outcome.needsSchedule,
		})
	}
	return pending, nil
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

// setWLocked writes a value to the versioned data structures at the current version.
func (s *shard) setWLocked(key string, value []byte) {
	// versionDiffs holds the key only until this version retires, when the whole map is dropped, so
	// the caller's string can be stored as it stands.
	s.versionDiffs[s.currentVersion][key] = value

	deque, ok := s.versionedData[key]
	if !ok {
		deque = structures.NewDeque[versionedValue]()
		// Cloned, unlike above, because this entry outlives the version that created it: a key that
		// keeps being written is never dropped, and Go leaves a map's original key in place on
		// reassignment, so this exact string is what the entry holds from here on. Callers may hand
		// in a string carved from a shared buffer, in which case keeping it would pin that whole
		// buffer for the life of the entry. The copy is per key new to this shard, not per write.
		s.versionedData[strings.Clone(key)] = deque
	}
	if deque.IsEmpty() || deque.PeekBack().version < s.currentVersion {
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	} else {
		deque.PopBack()
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	}
}

// BatchSet sets the values for a batch of keys at the current version.
//
// A write to a shard that is out of service is refused: it would land in versioned data that no
// lifecycle runner remains to flush, and so be discarded silently.
func (s *shard) BatchSet(entries []BatchKVPair) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return err
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

// batchUpdate writes a new value for each key named by indices, obtained by handing that key's
// prior value to updater. Refused on a shard that is out of service, for the reason given on
// BatchSet.
//
// The prior value is resolved with the same two-pass classification batchGet uses, and for the
// same reason: a batch that took the exclusive lock for its whole length would stall every read on
// this shard. So the exclusive holds here cover only the classification of what the fast pass
// could not resolve, and the writes themselves. The DB reads and every call into updater happen
// outside both.
func (s *shard) batchUpdate(
	keys []string,
	indices []int,
	updater BatchUpdater,
	version uint64,
	priorValues [][]byte,
	newValues [][]byte,
) error {
	unresolved, hits, err := s.attemptFastBatchGetUnlocked(keys, indices, priorValues, version)
	if err != nil {
		return err
	}

	var pending []pendingRead
	if len(unresolved) > 0 {
		pending, err = s.batchGetRemainingUnlocked(keys, unresolved, priorValues, version, &hits)
		if err != nil {
			return err
		}
	}

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	if err := s.cache.ResolveBatchUnlocked(pending, priorValues); err != nil {
		return err
	}

	// Outside every lock: updater is caller code of unknown cost, and holding the shard exclusively
	// across it is what the two-pass split above exists to avoid.
	for _, index := range indices {
		newValues[index], err = updater.NewValueFor(keys[index], priorValues[index])
		if err != nil {
			return fmt.Errorf("new value for key: %w", err)
		}
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return err
	}
	for _, index := range indices {
		// A nil new value is stored as a nil-valued (tombstone) entry at the current version.
		s.setWLocked(keys[index], newValues[index])
	}
	return nil
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

	return newVersion, err
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

	// A read lock suffices, and it matters: sort jobs for different versions call this concurrently.
	// Nothing here mutates the shard, and the maps handed back are frozen — only versionDiffs at the
	// current version is ever written to, so a version stops changing the moment it is no longer current.
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
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Same reason the read paths check it: a shard taken out of service cannot vouch for its data,
	// and an iterator is just a bulk read.
	if err := s.cache.ErrIfOutOfServiceRLocked(); err != nil {
		return nil, err
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
		out = append(out, kvPair{
			key:   []byte(key),
			value: deque.PeekBack().value,
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
