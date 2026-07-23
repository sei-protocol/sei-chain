package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// readFromDB reads a single key from the underlying database, returning (nil, false, nil) if the
// key is not found and reserving errors for actual failures (e.g. I/O errors).
func (s *shard) readFromDB(key []byte) (value []byte, found bool, err error) {
	val, err := s.db.Get(key)
	if err != nil {
		if errors.Is(err, errorutils.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read value from database: %w", err)
	}
	return val, true, nil
}

// A single shard of a SnapshotEngine.
type shard struct {
	ctx    context.Context
	config *SnapshotEngineConfig

	// The underlying key-value database.
	db types.KeyValueDB

	// A lock to protect the shard's data.
	lock sync.Mutex

	// Data at various versions. This is for data that has not yet been flushed down into the DB.
	versionedData map[string] /* key */ *structures.Deque[versionedValue] /* values at various versions */

	// For each version, contains the values set in that version.  If a value is set more than once
	// in a version, only the last value is stored. Although possible to find the value at a specifc
	// version by iterating over this map, it is much more efficient to use the versionedData map.
	versionDiffs map[uint64] /* version */ map[string] /* key */ []byte /* value */

	// A cache containing data as it is in the DB.
	dbCache map[string]*dbCacheEntry

	// Organizes data in dbCache for LRU eviction.
	dbCacheGCQueue *structures.LRUQueue

	// A pool for asynchronous reads.
	readPool threading.Pool

	// The maximum size of the db cache, in bytes.
	maxSize uint64

	// SnapshotEngine-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *SnapshotEngineMetrics

	// The current version number.
	currentVersion uint64

	// The oldest version number kept in versionedData.
	oldestVersion uint64
}

// The result of a read from the underlying database.
type readResult struct {
	value []byte
	err   error
}

// The status of a value in the cache.
type valueStatus int

const (
	// The value is not known and we are not currently attempting to find it.
	statusUnknown valueStatus = 1
	// We've scheduled a read of the value but haven't yet finished the read.
	statusScheduled valueStatus = 2
	// The data is available.
	statusAvailable valueStatus = 3
	// We are aware that the value is deleted (special case of data being available).
	statusDeleted valueStatus = 4
	// A read of this value from the DB failed. The failure is permanent: the entry is never
	// retried and never enters the LRU queue (so it cannot be evicted and re-read), and all
	// future reads of the key deterministically observe the error. A retry that succeeded after
	// the original failure was propagated could fork the chain.
	statusFailed valueStatus = 5
)

// A single entry in the db cache. Records data for a single key.
type dbCacheEntry struct {
	// The parent shard that contains this entry.
	shard *shard

	// The current status of this entry.
	status valueStatus

	// The value, if known.
	value []byte

	// The error that permanently failed this entry. Non-nil if and only if status is statusFailed.
	err error

	// If the value is not available when we request it,
	// it will be written to this channel when it is available.
	valueChan chan readResult
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

/*
This implementation currently uses a single exlusive lock, as opposed to a RW lock. This is a lot simpler than
using a RW lock, but it comes at higher risk of contention under certain workloads. If this contention ever
becomes a problem, we might consider switching to a RW lock. Below is a potential implementation strategy
for converting to a RW lock:

- Create a background goroutine that is responsible for LRU eviction and updating the LRU.
- The eviction goroutine should periodically wake up, grab the lock, and do eviction.
- When Get() is called, the calling goroutine should grab a read lock and attempt to read the value.
    - If the value is present, send a message to the eviction goroutine over a channel (so it can update the LRU)
	  and return the value. In this way, many readers can read from this shard concurrently.
	- If the value is missing, drop the read lock and acquire a write lock. Then, handle the read
	  like we currently handle in the current implementation.
*/

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
) (*shard, error) {

	if maxSize == 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	versionDiffs := make(map[uint64]map[string][]byte)
	versionDiffs[1] = make(map[string][]byte) // versions start at 1

	return &shard{
		ctx:            ctx,
		config:         config,
		db:             db,
		readPool:       readPool,
		lock:           sync.Mutex{},
		dbCache:        make(map[string]*dbCacheEntry),
		dbCacheGCQueue: structures.NewLRUQueue(),
		maxSize:        maxSize,
		versionedData:  make(map[string]*structures.Deque[versionedValue]),
		versionDiffs:   versionDiffs,
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,
	}, nil
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
	s.lock.Lock()

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

	// If we reach this point, we didn't find a value for this version in the versioned data map.
	// Check the DB cache, and potentially read from the DB if it's not in memory.

	entry := s.getDBCacheEntryLocked(key, true)

	switch entry.status {
	case statusAvailable:
		return s.getAvailableLocked(entry, key, updateLru)
	case statusDeleted:
		return s.getDeletedLocked(key, updateLru)
	case statusScheduled:
		return s.getScheduledLocked(entry)
	case statusUnknown:
		return s.getUnknownLocked(entry, key)
	case statusFailed:
		return s.getFailedLocked(entry)
	default:
		s.lock.Unlock()
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// Handles Get for a key whose earlier DB read failed. The failure is permanent (see statusFailed).
// Releases the lock before returning.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) getFailedLocked(entry *dbCacheEntry) ([]byte, bool, error) {
	err := entry.err
	s.lock.Unlock()
	return nil, false, fmt.Errorf("an earlier read of this key failed: %w", err)
}

// Handles Get for a key whose value is already cached. Releases the lock
// before returning.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) getAvailableLocked(entry *dbCacheEntry, key []byte, updateLru bool) ([]byte, bool, error) {
	value := entry.value
	if updateLru {
		s.dbCacheGCQueue.Touch(key)
	}
	s.lock.Unlock()
	s.metrics.reportCacheHits(1)
	return value, true, nil
}

// Handles Get for a key known to be deleted. Releases the lock before
// returning.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) getDeletedLocked(key []byte, updateLru bool) ([]byte, bool, error) {
	if updateLru {
		s.dbCacheGCQueue.Touch(key)
	}
	s.lock.Unlock()
	s.metrics.reportCacheHits(1)
	return nil, false, nil
}

// Handles Get for a key with an in-flight read from another goroutine.
// Releases the lock before returning.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) getScheduledLocked(entry *dbCacheEntry) ([]byte, bool, error) {
	valueChan := entry.valueChan
	s.lock.Unlock()
	s.metrics.reportCacheMisses(1)
	startTime := time.Now()
	result, err := threading.InterruptiblePull(s.ctx, valueChan)
	s.metrics.reportCacheMissLatency(time.Since(startTime))
	if err != nil {
		return nil, false, fmt.Errorf("failed to pull value from channel: %w", err)
	}
	valueChan <- result // reload the channel in case there are other listeners
	if result.err != nil {
		return nil, false, fmt.Errorf("scheduled read failed: %w", result.err)
	}
	return result.value, result.value != nil, nil
}

// Handles Get for a key not yet read. Schedules the read and waits. Releases
// the lock before returning.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) getUnknownLocked(entry *dbCacheEntry, key []byte) ([]byte, bool, error) {
	entry.status = statusScheduled
	valueChan := make(chan readResult, 1)
	entry.valueChan = valueChan
	s.lock.Unlock()
	s.metrics.reportCacheMisses(1)
	startTime := time.Now()
	s.readPool.Submit(func() {
		value, _, readErr := s.readFromDB(key)
		entry.injectValue(key, readResult{value: value, err: readErr})
	})
	result, err := threading.InterruptiblePull(s.ctx, valueChan)
	s.metrics.reportCacheMissLatency(time.Since(startTime))
	if err != nil {
		return nil, false, fmt.Errorf("failed to pull value from channel: %w", err)
	}
	valueChan <- result // reload the channel in case there are other listeners
	if result.err != nil {
		return nil, false, fmt.Errorf("scheduled read failed: %w", result.err)
	}
	return result.value, result.value != nil, nil
}

// This method is called by the read scheduler when a value becomes available.
func (se *dbCacheEntry) injectValue(key []byte, result readResult) {
	se.shard.lock.Lock()

	if se.status == statusScheduled {
		if result.err != nil {
			// Latch the failure permanently (see statusFailed): the error has been propagated
			// to a caller, and a later retry that succeeded would be a fork risk. The entry is
			// deliberately kept out of the LRU queue so it can never be evicted and re-read.
			se.status = statusFailed
			se.err = result.err
		} else if result.value == nil {
			se.status = statusDeleted
			se.value = nil
			size := uint64(len(key)) + se.shard.config.EstimatedOverheadPerEntry
			se.shard.dbCacheGCQueue.Push(key, size)
			se.shard.evictLocked()
		} else {
			se.status = statusAvailable
			se.value = result.value
			size := uint64(len(key)) + uint64(len(result.value)) + se.shard.config.EstimatedOverheadPerEntry
			se.shard.dbCacheGCQueue.Push(key, size)
			se.shard.evictLocked()
		}
	}

	se.shard.lock.Unlock()

	se.valueChan <- result
}

// Get a cb cache entry for a given key.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) getDBCacheEntryLocked(key []byte, createIfMissing bool) *dbCacheEntry {
	if entry, ok := s.dbCache[string(key)]; ok {
		return entry
	}
	if !createIfMissing {
		return nil
	}
	entry := &dbCacheEntry{
		shard:  s,
		status: statusUnknown,
	}
	keyStr := string(key)
	s.dbCache[keyStr] = entry
	return entry
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
// Returns (value, true) if found in versioned data, (nil, false) if the dbCache should be consulted.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) lookupVersionedLocked(key string, version uint64) ([]byte, bool) {
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

// Tracks a key whose value is not yet available and must be waited on.
type pendingRead struct {
	key           string
	entry         *dbCacheEntry
	valueChan     chan readResult
	needsSchedule bool
	// Populated after the read completes, used by bulkInjectValues.
	result readResult
}

// BatchGet reads a batch of keys from the shard. Results are written into the provided map.
// BatchGet reads the given keys at the given version, returning a map (keyed by string(key)) of the
// keys that were found to their values. Not-found and deleted keys are absent from the map. Any read
// error fails the whole call and returns a nil map.
func (s *shard) BatchGet(keys [][]byte, version uint64) (map[string][]byte, error) {
	results := make(map[string][]byte, len(keys))
	pending := make([]pendingRead, 0, len(keys))
	var hits int64

	s.lock.Lock()

	if err := s.validateVersionLocked(version); err != nil {
		s.lock.Unlock()
		return nil, err
	}

	for _, key := range keys {
		keyStr := string(key)
		if value, found := s.lookupVersionedLocked(keyStr, version); found {
			// found includes tombstones (nil value); only non-nil values are real hits to return.
			if value != nil {
				results[keyStr] = value
			}
			hits++
			continue
		}

		entry := s.getDBCacheEntryLocked(key, true)

		switch entry.status {
		case statusAvailable:
			results[keyStr] = entry.value
			hits++
		case statusDeleted:
			// Known-deleted: resolved from cache, but not a found value.
			hits++
		case statusScheduled:
			pending = append(pending, pendingRead{
				key:       keyStr,
				entry:     entry,
				valueChan: entry.valueChan,
			})
		case statusUnknown:
			entry.status = statusScheduled
			valueChan := make(chan readResult, 1)
			entry.valueChan = valueChan
			pending = append(pending, pendingRead{
				key:           keyStr,
				entry:         entry,
				valueChan:     valueChan,
				needsSchedule: true,
			})
		case statusFailed:
			err := entry.err
			s.lock.Unlock()
			return nil, fmt.Errorf("an earlier read of key failed: %w", err)
		default:
			s.lock.Unlock()
			panic(fmt.Sprintf("unexpected status: %#v", entry.status))
		}
	}
	s.lock.Unlock()

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}
	if len(pending) == 0 {
		return results, nil
	}

	s.metrics.reportCacheMisses(int64(len(pending)))
	startTime := time.Now()

	for i := range pending {
		if pending[i].needsSchedule {
			p := &pending[i]
			s.readPool.Submit(func() {
				value, _, readErr := s.readFromDB([]byte(p.key))
				p.entry.valueChan <- readResult{value: value, err: readErr}
			})
		}
	}

	// Drain every pending read even if one fails: each scheduled read pushes exactly one token,
	// so the drain is bounded by reads already in flight, and it leaves every entry in a terminal
	// state (available/deleted/failed) via bulkInjectValues below. Abandoning the drain on the
	// first error would strand the remaining entries in statusScheduled with unpopulated results.
	var firstErr error
	for i := range pending {
		result, err := threading.InterruptiblePull(s.ctx, pending[i].valueChan)
		if err != nil {
			// Context cancellation is a hard teardown: post-shutdown entry state is
			// unobservable, and draining could block on reads that never complete while the
			// pool is being torn down, so bail immediately.
			return nil, fmt.Errorf("failed to pull value from channel: %w", err)
		}
		pending[i].valueChan <- result
		pending[i].result = result

		if result.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to read key from database: %w", result.err)
			}
			continue
		}
		if result.value != nil {
			results[pending[i].key] = result.value
		}
	}

	s.metrics.reportCacheMissLatency(time.Since(startTime))
	go s.bulkInjectValues(pending)

	if firstErr != nil {
		// DB errors are fatal; fail the whole batch.
		return nil, firstErr
	}
	return results, nil
}

// Applies deferred cache updates for a batch of reads under a single lock acquisition.
func (s *shard) bulkInjectValues(reads []pendingRead) {
	s.lock.Lock()
	for i := range reads {
		entry := reads[i].entry
		if entry.status != statusScheduled {
			continue
		}
		result := reads[i].result
		if result.err != nil {
			// Latch the failure permanently (see statusFailed): the error has been propagated
			// to a caller, and a later retry that succeeded would be a fork risk. The entry is
			// deliberately kept out of the LRU queue so it can never be evicted and re-read.
			entry.status = statusFailed
			entry.err = result.err
		} else if result.value == nil {
			entry.status = statusDeleted
			entry.value = nil
			size := uint64(len(reads[i].key)) + s.config.EstimatedOverheadPerEntry
			s.dbCacheGCQueue.Push([]byte(reads[i].key), size)
		} else {
			entry.status = statusAvailable
			entry.value = result.value
			size := uint64(len(reads[i].key)) + uint64(len(result.value)) +
				s.config.EstimatedOverheadPerEntry
			s.dbCacheGCQueue.Push([]byte(reads[i].key), size)
		}
	}
	s.evictLocked()
	s.lock.Unlock()
}

// Evicts least recently used entries until the cache is within its size budget.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) evictLocked() {
	for s.dbCacheGCQueue.GetTotalSize() > s.maxSize {
		next := s.dbCacheGCQueue.PopLeastRecentlyUsed()
		delete(s.dbCache, next)
	}
}

// getSizeInfo returns the current size (bytes) and entry count under the shard lock.
func (s *shard) getSizeInfo() (bytes uint64, entries uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.dbCacheGCQueue.GetTotalSize(), s.dbCacheGCQueue.GetCount()
}

// Set sets the value for the given key at the current version.
func (s *shard) Set(key []byte, value []byte) {
	s.lock.Lock()
	s.setLocked(key, value)
	s.lock.Unlock()
}

// setLocked writes a value to the versioned data structures at the current version.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) setLocked(key []byte, value []byte) {
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

// Set a value.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) setInDBCacheLocked(key []byte, value []byte) {
	entry := s.getDBCacheEntryLocked(key, true)
	entry.status = statusAvailable
	entry.value = value
	// Overwriting a failed entry is deliberate: this data comes from the engine's own retired
	// writes, not from the failed DB read, and the original error has already been propagated.
	entry.err = nil

	size := uint64(len(key)) + uint64(len(value)) + s.config.EstimatedOverheadPerEntry
	s.dbCacheGCQueue.Push(key, size)
}

// BatchSet sets the values for a batch of keys at the current version.
func (s *shard) BatchSet(entries []*proto.KVPair) {
	s.lock.Lock()
	for i := range entries {
		if entries[i].Delete {
			// A delete is stored as a nil-valued (tombstone) entry at the current version.
			s.setLocked(entries[i].Key, nil)
		} else {
			s.setLocked(entries[i].Key, entries[i].Value)
		}
	}
	s.lock.Unlock()
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) {
	s.Set(key, nil)
}

// Delete a value.
//
// The Locked postfix indicates that the caller must hold the shard lock.
func (s *shard) deleteInDBCacheLocked(key []byte) {
	entry := s.getDBCacheEntryLocked(key, false)
	if entry == nil {
		// Key is not in the cache, so nothing to do.
		return
	}
	entry.status = statusDeleted
	entry.value = nil
	// Overwriting a failed entry is deliberate: this tombstone comes from the engine's own
	// retired writes, not from the failed DB read, and the original error has already been
	// propagated.
	entry.err = nil

	size := uint64(len(key)) + s.config.EstimatedOverheadPerEntry
	s.dbCacheGCQueue.Push(key, size)
}

// Take a snapshot of the state at the current version. All future updates will be applied to the next version.
// The value returned is the new version number (for sanity checking).
func (s *shard) Snapshot() uint64 {
	s.lock.Lock()

	newVersion := s.currentVersion + 1
	s.currentVersion = newVersion

	s.versionDiffs[newVersion] = make(map[string][]byte)

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

// materializeOverridesAtVersion returns every in-memory override visible to this shard
// at the given version. The result is unsorted.
//
// Keys whose most recent entry is at a version greater than version are
// skipped (the key did not yet exist in the cache at that version).
func (s *shard) materializeOverridesAtVersion(version uint64) ([]kvPair, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.validateVersionLocked(version); err != nil {
		return nil, err
	}

	out := make([]kvPair, 0, len(s.versionedData))
	for key := range s.versionedData {
		value, found := s.lookupVersionedLocked(key, version)
		if !found {
			continue
		}
		out = append(out, kvPair{
			key:   []byte(key),
			value: value,
		})
	}
	return out, nil
}

// Drop versions, pushing their data down into the DB cache. The first version to drop must be equal to the
// oldest version currently being tracked.
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

	// Insert the combined data into the cache.
	for k, v := range combinedData {
		if v == nil {
			s.deleteInDBCacheLocked([]byte(k))
		} else {
			s.setInDBCacheLocked([]byte(k), v)
		}
	}

	// These insertions may have caused the DB read-cache to exceed its size budget, do necessary
	// evictions. setInDBCacheLocked does not evict on its own, so this is the enforcement point for
	// the bulk insert above.
	s.evictLocked()

	// Update the oldset version.
	s.oldestVersion = lastVersion

	return nil
}
