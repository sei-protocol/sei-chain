package snapshot

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// A single shard of a SnapshotEngine. The shard owns the MVCC layer: versioned in-memory data
// awaiting flush, per-version diffs, and version bookkeeping. Reads that miss the versioned data
// fall through to the shard's read-through DB cache (see readCache).
//
// A shard that is out of service refuses reads and writes, reporting the failure that stopped it.
// Two things put it there:
//
//   - The engine was shut down. Only reachable by calling Close concurrently with an operation that
//     touches a shard, which is illegal.
//   - The database crashed. Database failures are fatal and are never recovered from, so every shard
//     goes out of service, not just the one that saw the failure.
type shard struct {
	// A lock to protect the shard's data. Shared with the read cache (see the cache field).
	//
	// A read that resolves from versioned data or from a cached entry needs only the read side, which
	// is why recency is recorded atomically rather than by reordering a queue. A read that misses has
	// to create an entry and schedule a DB read, so it takes the write side.
	lock sync.RWMutex

	// Data at various versions. This is for data that has not yet been flushed down into the DB.
	versionedData map[string] /* key */ versionHistory /* values at various versions */

	// For each version, contains the values set in that version.  If a value is set more than once
	// in a version, only the last value is stored. Although possible to find the value at a specifc
	// version by iterating over this map, it is much more efficient to use the versionedData map.
	versionDiffs map[uint64] /* version */ map[string] /* key */ []byte /* value */

	// The read-through DB cache backing this shard. A passive component sharing this shard's
	// lock: its xxxLocked methods require the lock held, while its resolve methods and background
	// read-completion paths manage their own synchronization. The cache never calls outward while
	// holding the lock — it never calls into the shard at all, and its one call into the engine
	// (reportReadFailure, which acquires versionLock) is made only after releasing the lock — so
	// nesting cache calls under the shard lock cannot deadlock. See readCache.
	cache *readCache

	// SnapshotEngine-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *SnapshotEngineMetrics

	// The current version number.
	currentVersion uint64

	// The oldest version number kept in versionedData.
	oldestVersion uint64

	// How many retired keys to migrate per acquisition of lock. See
	// SnapshotEngineConfig.RetirementChunkSize.
	retirementChunkSize int

	// The number of iterators currently reading this shard. Close reports a non-zero count as a
	// leaked iterator, since reading one after the database has closed is undefined behaviour (see
	// SnapshotEngine.Close).
	//
	// Guarded by lock.
	openIterators uint64
}

// A single value at a specific version.
type versionedValue struct {
	// The value.
	value []byte
	// The engine version at which this value was written. Note that this is NOT the same
	// as block height, this is just a version number that monotonically increases over the lifetime
	// of a snapshot engine instance.
	version uint64
}

// versionHistory holds every value a key has taken across the shard's un-retired versions, ordered
// oldest to newest.
//
// It is stored by value in versionedData, and the newest value is held inline, because a key is
// almost always written in only one un-retired version: a key written once costs no allocation at
// all. Only a key written in a second version allocates, spilling its earlier values into older.
type versionHistory struct {
	// The value at the most recent version this key was written at.
	newest versionedValue

	// Values at earlier versions, oldest first. Nil until the key is written at a second version.
	older *structures.Deque[versionedValue]
}

// olderLen returns how many values older holds, treating a nil deque as empty.
func (h versionHistory) olderLen() int {
	if h.older == nil {
		return 0
	}
	return h.older.Len()
}

// len returns how many versions this history holds. A history in the map always holds at least one.
func (h versionHistory) len() int {
	return h.olderLen() + 1
}

// get returns the i'th value, counting from the oldest.
func (h versionHistory) get(i int) versionedValue {
	if i < h.olderLen() {
		return h.older.Get(i)
	}
	return h.newest
}

// oldest returns the value at the earliest version this history holds.
func (h versionHistory) oldest() versionedValue {
	if h.olderLen() == 0 {
		return h.newest
	}
	return h.older.PeekFront()
}

// set records value at the current version, returning the updated history. A repeat write at the
// version already held replaces it rather than appending, so a history never holds one version
// twice.
func (h versionHistory) set(value versionedValue) versionHistory {
	if h.newest.version == value.version {
		h.newest = value
		return h
	}
	if h.older == nil {
		h.older = structures.NewDequeWithCapacity[versionedValue](1)
	}
	h.older.PushBack(h.newest)
	h.newest = value
	return h
}

// dropOlderThan discards every value written before version, returning the updated history and
// whether anything is left. A history with nothing left must be removed from versionedData.
func (h versionHistory) dropOlderThan(version uint64) (versionHistory, bool) {
	for h.olderLen() > 0 && h.older.PeekFront().version < version {
		h.older.PopFront()
	}
	if h.olderLen() == 0 && h.newest.version < version {
		return versionHistory{}, false
	}
	return h, true
}

// Creates a new Shard.
func NewShard(
	ctx context.Context,
	config *SnapshotEngineConfig,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for asynchronous reads.
	readPool threading.Pool,
	// The maximum size of this shard, in bytes.
	maxSize uint64,
	// Maps the context cancellation observed by a blocked read to the engine's shutdown error
	// (the latched fatal error, or ErrEngineClosed on a clean close). Called only after ctx has
	// been cancelled.
	shutdownError func() error,
	// Reports a failed DB read to the engine, which bricks and stops serving reads.
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
		versionedData:  make(map[string]versionHistory),
		versionDiffs:   versionDiffs,
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,

		retirementChunkSize: config.RetirementChunkSize,
	}
	s.cache = newReadCache(
		ctx, db, readPool, &s.lock, maxSize, config.EstimatedOverheadPerEntry, shutdownError, reportReadFailure)
	return s, nil
}

// takeOutOfService stops this shard from serving reads and accepting writes, reporting err as the
// cause. Called on every shard when the engine shuts down, so a failure anywhere stops every shard.
func (s *shard) takeOutOfService(err error) {
	s.lock.Lock()
	s.cache.takeOutOfServiceLocked(err)
	s.lock.Unlock()
}

// Get returns the value for the given key, or (nil, false, nil) if not found at the given version.
func (s *shard) Get(
	// The key to get.
	key []byte,
	// The version of the data to get.
	version uint64,
	// If true, the LRU queue will be updated. If false, the LRU queue will not be updated.
	// Useful for when an operation is performed multiple times in close succession on the same key,
	// since it requires non-zero overhead to do so with little benefit.
	updateLru bool,
) ([]byte, bool, error) {
	if value, found, err, done := s.getShared(key, version, updateLru); done {
		return value, found, err
	}

	// The read was not resolvable without mutating: classify it against the DB read-cache under the
	// exclusive lock, then complete the read (which may schedule a DB read and block) outside it.
	//
	// The whole classification is redone rather than carried over from the shared attempt, because the
	// lock was released in between and another reader may have scheduled or completed this key.
	s.lock.Lock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.outOfServiceLocked(); err != nil {
		s.lock.Unlock()
		return nil, false, err
	}

	if err := s.validateVersionLocked(version); err != nil {
		s.lock.Unlock()
		return nil, false, err
	}

	// First, check to see if we have this value in the versioned data map.
	if value, found := s.lookupVersionedLocked(string(key), version); found {
		s.lock.Unlock()
		s.metrics.reportCacheHits(1)
		return value, value != nil, nil
	}

	outcome := s.cache.lookupLocked(key, updateLru)
	s.lock.Unlock()

	return s.cache.resolve(key, outcome)
}

// getShared attempts a read while holding only the shared lock, reporting done when it succeeded.
//
// This is the path nearly every read takes, and it exists because a hit mutates nothing: the
// out-of-service flag, the version bounds, the versioned data and the cache's entry map are all read,
// and the recency stamp is atomic precisely so that recording it does not require exclusivity. A read
// that misses does have to mutate — an entry is created and a DB read scheduled — and is left to the
// caller to redo exclusively.
func (s *shard) getShared(
	key []byte,
	version uint64,
	updateLru bool,
) (value []byte, found bool, err error, done bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if err := s.cache.outOfServiceLocked(); err != nil {
		return nil, false, err, true
	}
	if err := s.validateVersionLocked(version); err != nil {
		return nil, false, err, true
	}

	if value, found := s.lookupVersionedLocked(string(key), version); found {
		s.metrics.reportCacheHits(1)
		return value, value != nil, nil, true
	}

	outcome, ok := s.cache.lookupSharedLocked(key, updateLru)
	if !ok {
		return nil, false, nil, false
	}

	// Safe under the shared lock: an immediate outcome carries its value already, so resolve only
	// reports the hit and returns without blocking or touching the cache again.
	value, found, err = s.cache.resolve(key, outcome)
	return value, found, err, true
}

// validateVersionLocked checks that the given version is within the valid range.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) validateVersionLocked(version uint64) error {
	if version < s.oldestVersion {
		return fmt.Errorf("version (%d) is less than the oldest version (%d)", version, s.oldestVersion)
	}
	if version > s.currentVersion {
		return fmt.Errorf("version (%d) is greater than the current version (%d)", version, s.currentVersion)
	}
	return nil
}

// lookupVersionedLocked checks versioned data for a key at the given version.
// Returns (value, true) if found in versioned data, (nil, false) if the read cache should be
// consulted.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) lookupVersionedLocked(key string, version uint64) ([]byte, bool) {
	history, ok := s.versionedData[key]
	if !ok {
		return nil, false
	}
	if version == s.oldestVersion {
		next := history.oldest()
		if next.version == version {
			return next.value, true
		}
		return nil, false
	}
	for i := history.len() - 1; i >= 0; i-- {
		next := history.get(i)
		if next.version <= version {
			return next.value, true
		}
	}
	return nil, false
}

// batchGetInto reads the keys named by indices at the given version, writing each key's value into
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
// whole length. That matters more here than for a single read: batches arrive from the hasher while
// execution is running, and Go's RWMutex queues arriving readers behind a waiting writer, so one long
// exclusive hold stalls every read on this shard and convoys those that follow it. The first pass takes
// the shared lock and resolves everything already present; only keys it could not resolve reach the
// second, which needs the exclusive lock to create entries and schedule DB reads.
func (s *shard) batchGetInto(keys []string, indices []int, values [][]byte, version uint64) error {
	unresolved, hits, err := s.batchGetSharedInto(keys, indices, values, version)
	if err != nil {
		return err
	}

	var pending []pendingRead
	if len(unresolved) > 0 {
		pending, err = s.batchGetExclusiveInto(keys, unresolved, values, version, &hits)
		if err != nil {
			return err
		}
	}

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	// DB errors are fatal; they fail the whole batch.
	return s.cache.resolveBatch(pending, values)
}

// batchGetSharedInto resolves the keys it can under the shared lock, returning the indices of those it
// could not.
func (s *shard) batchGetSharedInto(
	keys []string,
	indices []int,
	values [][]byte,
	version uint64,
) (unresolved []int, hits int64, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Checked ahead of the versioned data so that a shard taken out of service refuses every read,
	// not just those that would have reached the DB.
	if err := s.cache.outOfServiceLocked(); err != nil {
		return nil, 0, err
	}

	if err := s.validateVersionLocked(version); err != nil {
		return nil, 0, err
	}

	for _, index := range indices {
		key := keys[index]
		if value, found := s.lookupVersionedLocked(key, version); found {
			// found includes tombstones, whose nil value lands as the not-found the caller reads it as.
			values[index] = value
			hits++
			continue
		}

		// The batch path never records recency on hits, hence updateLru=false.
		outcome, ok := s.cache.lookupSharedStringLocked(key, false)
		if ok {
			// Resolved from cache. A deleted key carries a nil value, as above.
			values[index] = outcome.value
			hits++
			continue
		}
		unresolved = append(unresolved, index)
	}
	return unresolved, hits, nil
}

// batchGetExclusiveInto classifies the keys the shared pass could not, creating entries and scheduling
// DB reads as needed.
//
// The whole classification is redone for these keys rather than carried over, because the lock was
// released in between and another reader may have scheduled or completed any of them.
func (s *shard) batchGetExclusiveInto(
	keys []string,
	indices []int,
	values [][]byte,
	version uint64,
	hits *int64,
) ([]pendingRead, error) {
	pending := make([]pendingRead, 0, len(indices))

	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.outOfServiceLocked(); err != nil {
		return nil, err
	}

	if err := s.validateVersionLocked(version); err != nil {
		return nil, err
	}

	for _, index := range indices {
		key := keys[index]
		if value, found := s.lookupVersionedLocked(key, version); found {
			values[index] = value
			*hits++
			continue
		}

		outcome := s.cache.lookupStringLocked(key, false)
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

// getSizeInfo returns the current cache size (bytes) and entry count under the shard lock.
func (s *shard) getSizeInfo() (bytes uint64, entries uint64) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.cache.sizeInfoLocked()
}

// iteratorOpened records that an iterator is reading this shard. Balanced by exactly one
// iteratorClosed.
func (s *shard) iteratorOpened() {
	s.lock.Lock()
	s.openIterators++
	s.lock.Unlock()
}

// iteratorClosed records that an iterator reading this shard has been closed.
func (s *shard) iteratorClosed() {
	s.lock.Lock()
	s.openIterators--
	s.lock.Unlock()
}

// Set sets the value for the given key at the current version.
//
// A write to a shard that is out of service is refused: it would land in versioned data that no
// lifecycle runner remains to flush, and so be discarded silently.
func (s *shard) Set(key []byte, value []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.cache.outOfServiceLocked(); err != nil {
		return err
	}
	s.setLocked(key, value)
	return nil
}

// setLocked writes a value to the versioned data structures at the current version.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) setLocked(key []byte, value []byte) {
	s.setLockedString(string(key), value)
}

// setLockedString is setLocked for a key already held as a string.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) setLockedString(key string, value []byte) {
	// versionDiffs holds the key only until this version retires, when the whole map is dropped, so
	// the caller's string can be stored as it stands.
	s.versionDiffs[s.currentVersion][key] = value

	written := versionedValue{version: s.currentVersion, value: value}
	// A key seen for the first time in this version window starts a history holding only this
	// value. Going through set would be wrong as well as wasteful: the zero history's newest is a
	// nil value at version 0, which set would preserve as a real earlier value.
	history, ok := s.versionedData[key]
	if !ok {
		// Cloned, unlike above, because this entry outlives the version that created it: a key that
		// keeps being written is never dropped, and Go leaves a map's original key in place on
		// reassignment, so this exact string is what the entry holds from here on. Callers may hand in
		// a string carved from a shared buffer, in which case keeping it would pin that whole buffer
		// for the life of the entry. The copy is per key new to this shard, not per write.
		s.versionedData[strings.Clone(key)] = versionHistory{newest: written}
		return
	}
	// The entry already exists, so this assignment does not retain a new key.
	s.versionedData[key] = history.set(written)
}

// BatchSet sets the values for a batch of keys at the current version. Refused on a shard that is
// out of service, for the reason given on Set.
func (s *shard) BatchSet(entries []*proto.KVPair) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.outOfServiceLocked(); err != nil {
		return err
	}
	for i := range entries {
		if entries[i].Delete {
			// A delete is stored as a nil-valued (tombstone) entry at the current version.
			s.setLocked(entries[i].Key, nil)
		} else {
			s.setLocked(entries[i].Key, entries[i].Value)
		}
	}
	return nil
}

// batchSetStringAt applies the updates named by indices, which index into updates. Refused on a
// shard that is out of service, for the reason given on Set.
//
// Taking the whole batch plus the indices belonging to this shard, rather than a slice of just this
// shard's updates, is what lets the caller bucket a batch by word-sized indices instead of copying
// every update into a per-shard buffer.
func (s *shard) batchSetStringAt(updates []StringKVPair, indices []int) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Checked once for the whole batch rather than per key: it cannot change while we hold the lock.
	if err := s.cache.outOfServiceLocked(); err != nil {
		return err
	}
	for _, i := range indices {
		update := &updates[i]
		if update.Delete {
			// A delete is stored as a nil-valued (tombstone) entry at the current version.
			s.setLockedString(update.Key, nil)
			continue
		}
		s.setLockedString(update.Key, update.Value)
	}
	return nil
}

// batchUpdateAt writes a new value for each key named by indices, obtained by handing that key's prior
// value to updater. Refused on a shard that is out of service, for the reason given on Set.
//
// The prior value is resolved with the same two-pass classification batchGetInto uses, and for the same
// reason: a batch that took the exclusive lock for its whole length would stall every read on this shard.
// So the exclusive holds here cover only the classification of what the shared pass could not resolve,
// and the writes themselves. The DB reads and every call into updater happen outside both.
func (s *shard) batchUpdateAt(
	keys []string,
	indices []int,
	updater BatchUpdater,
	version uint64,
	priorValues [][]byte,
	newValues [][]byte,
) error {
	unresolved, hits, err := s.batchGetSharedInto(keys, indices, priorValues, version)
	if err != nil {
		return err
	}

	var pending []pendingRead
	if len(unresolved) > 0 {
		pending, err = s.batchGetExclusiveInto(keys, unresolved, priorValues, version, &hits)
		if err != nil {
			return err
		}
	}

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	if err := s.cache.resolveBatch(pending, priorValues); err != nil {
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
	if err := s.cache.outOfServiceLocked(); err != nil {
		return err
	}
	for _, index := range indices {
		// A nil new value is stored as a nil-valued (tombstone) entry at the current version.
		s.setLockedString(keys[index], newValues[index])
	}
	return nil
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) error {
	return s.Set(key, nil)
}

// Commit seals the current version; all future updates will be applied to the next version.
// The value returned is the new version number (for sanity checking).
func (s *shard) Commit() uint64 {
	s.lock.Lock()

	newVersion := s.currentVersion + 1

	// Sized at twice the version just sealed. The map is created here but filled by the next
	// version's writes, and growing it there means rehashing every key written so far, on the
	// thread doing the writing and under this shard's lock.
	s.versionDiffs[newVersion] = make(map[string][]byte, 2*len(s.versionDiffs[s.currentVersion]))
	s.currentVersion = newVersion

	s.lock.Unlock()

	return newVersion
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

	s.lock.Lock()
	defer s.lock.Unlock()

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

// materializeCurrentOverrides returns the in-memory overrides in this shard at the current version
// whose keys fall within [lowerBound, upperBound). A nil bound is unbounded on that side. The result
// is unsorted.
//
// Because the target is always the current version, each key resolves to the back of its deque —
// no version scan is needed, unlike lookupVersionedLocked, which serves reads at older versions.
func (s *shard) materializeCurrentOverrides(lowerBound []byte, upperBound []byte) ([]kvPair, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Same reason the read paths check it: a shard taken out of service cannot vouch for its data,
	// and an iterator is just a bulk read.
	if err := s.cache.outOfServiceLocked(); err != nil {
		return nil, err
	}

	out := make([]kvPair, 0, len(s.versionedData))
	for key, history := range s.versionedData {
		if lowerBound != nil && key < string(lowerBound) {
			continue
		}
		if upperBound != nil && key >= string(upperBound) {
			continue
		}
		out = append(out, kvPair{
			key:   []byte(key),
			value: history.newest.value,
		})
	}
	return out, nil
}

// Drop versions, pushing their data down into the read cache. The first version to drop must be
// equal to the oldest version currently being tracked.
//
// The lock is released and retaken every RetirementChunkSize keys rather than held for the whole set, so a
// read can arrive with the migration half done. Three things make that safe:
//
// Each key moves atomically. Its removal from the versioned data and its arrival in the cache happen
// under one hold, so a read finds it in one place or the other, never neither.
//
// The version bounds do not move until every key has. lookupVersionedLocked reads oldestVersion as a
// promise that nothing below it is left in the versioned data, so advancing it early would send a read
// at lastVersion to a cache that had not yet received the entry it wanted.
//
// A version is only retired once it is flushed (see scanForRetirementEligibilityLocked), so every key
// here is already durable. A read that reaches the database rather than the cache is therefore slower
// but never wrong.
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

	if firstVersion != s.oldestVersion {
		s.lock.Unlock()
		return fmt.Errorf("firstVersion (%d) must be equal to the oldest version (%d)",
			firstVersion, s.oldestVersion)
	}
	if lastVersion > s.currentVersion {
		s.lock.Unlock()
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

	s.migrateRetiredDataLocked(combinedData, lastVersion)

	// Advanced only once every key has moved. lookupVersionedLocked treats a read at oldestVersion as a
	// read of the oldest entry it still holds, on the understanding that everything below oldestVersion
	// has already been migrated out — so advancing this while keys were still to move would make a read
	// at lastVersion miss the entry it needs and fall through to a cache that has not received it yet.
	s.oldestVersion = lastVersion

	s.lock.Unlock()
	return nil
}

// migrateRetiredDataLocked moves the retired versions' data out of the versioned map and down into the
// read cache, in chunks, releasing the lock at each chunk boundary.
//
// A key whose newest write was in the retired range leaves the versioned map entirely and is served from
// the cache from then on; a key written since keeps the remainder of its history.
//
// The Locked postfix indicates that the caller must hold the lock; it is still held on return, having
// been handed over and retaken in between.
func (s *shard) migrateRetiredDataLocked(retired map[string][]byte, lastVersion uint64) {
	remainingInChunk := s.retirementChunkSize
	for key, value := range retired {
		history, remaining := s.versionedData[key].dropOlderThan(lastVersion)
		if remaining {
			s.versionedData[key] = history
		} else {
			delete(s.versionedData, key)
		}

		// The per-key form rather than putRetiredLocked, which would evict once per chunk. Eviction is
		// left to the single pass below.
		if value == nil {
			s.cache.deleteRetiredLocked(key)
		} else {
			s.cache.setRetiredLocked(key, value)
		}

		remainingInChunk--
		if remainingInChunk == 0 {
			// Handing the lock over mid-migration is the point of chunking: readers waiting on this
			// shard get in here rather than behind the whole retirement. Not a no-op even though the
			// lock is retaken immediately — RWMutex.Unlock releases every reader already waiting, and
			// the Lock below then waits for them, so the readers drain rather than this barging back in.
			s.lock.Unlock()
			s.lock.Lock()
			remainingInChunk = s.retirementChunkSize
		}
	}

	// The insertions above may have taken the cache over its size budget, and the per-key form does not
	// evict, so this is the enforcement point for the whole migration.
	s.cache.evictLocked(s.cache.hardCapLocked())
}

// maintainCache advances the cache's epoch and brings it back within its size budget.
//
// Eviction is batched here rather than done by whichever read happened to miss, so that a block's
// worth of it happens in one pass. Between passes the cache may exceed its budget by the slack
// evictionSlackDivisor allows; insertions enforce a hard ceiling above that if a single block
// overshoots.
func (s *shard) maintainCache() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.cache.maintainLocked()
}
