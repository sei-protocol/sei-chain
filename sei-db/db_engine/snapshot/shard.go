package snapshot

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// A single shard of a SnapshotEngine. The shard owns the MVCC layer: versioned in-memory data
// awaiting flush, per-version diffs, and version bookkeeping. Reads that miss the versioned data
// fall through to the shard's read-through DB cache (see readCache).
type shard struct {
	// A lock to protect the shard's data. Shared with the read cache (see the cache field).
	lock sync.Mutex

	// Data at various versions. This is for data that has not yet been flushed down into the DB.
	versionedData map[string] /* key */ *structures.Deque[versionedValue] /* values at various versions */

	// For each version, contains the values set in that version.  If a value is set more than once
	// in a version, only the last value is stored. Although possible to find the value at a specifc
	// version by iterating over this map, it is much more efficient to use the versionedData map.
	versionDiffs map[uint64] /* version */ map[string] /* key */ []byte /* value */

	// The read-through DB cache backing this shard. A passive component sharing this shard's
	// lock: its xxxLocked methods require the lock held, while its resolve methods and background
	// read-completion paths manage their own synchronization. The cache never calls back into the
	// shard, so nesting cache calls under the shard lock cannot deadlock. See readCache.
	cache *readCache

	// SnapshotEngine-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *SnapshotEngineMetrics

	// The current version number.
	currentVersion uint64

	// The oldest version number kept in versionedData.
	oldestVersion uint64
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
) (*shard, error) {

	if maxSize == 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}
	if shutdownError == nil {
		return nil, fmt.Errorf("shutdownError must be non-nil")
	}

	versionDiffs := make(map[uint64]map[string][]byte)
	versionDiffs[1] = make(map[string][]byte) // versions start at 1

	s := &shard{
		versionedData:  make(map[string]*structures.Deque[versionedValue]),
		versionDiffs:   versionDiffs,
		currentVersion: 1, // important: versions start at 1, not 0, to allow (version - 1) without underflow
		oldestVersion:  1,
	}
	s.cache = newReadCache(ctx, db, readPool, &s.lock, maxSize, config.EstimatedOverheadPerEntry, shutdownError)
	return s, nil
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

	// Not in the versioned data map: classify against the DB read-cache under the same lock
	// grab, then complete the read (which may schedule a DB read and block) outside the lock.
	outcome := s.cache.lookupLocked(key, updateLru)
	s.lock.Unlock()

	return s.cache.resolve(key, outcome)
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

// BatchGet reads the given keys at the given version, returning a map (keyed by string(key)) of the
// keys that were found to their values. Not-found and deleted keys are absent from the map. Any read
// error fails the whole call and returns a nil map.
func (s *shard) BatchGet(keys [][]byte, version uint64) (map[string][]byte, error) {
	results := make(map[string][]byte, len(keys))
	pending := make([]pendingRead, 0, len(keys))
	var hits int64
	var firstErr error

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

		// The batch path never touches the LRU queue on hits, hence updateLru=false.
		outcome := s.cache.lookupLocked(key, false)
		switch {
		case outcome.immediate && outcome.err != nil:
			if firstErr == nil {
				// Latch the error but keep classifying: earlier keys in this batch may already
				// have been flipped to statusScheduled, and their reads must still be scheduled
				// and drained by resolveBatch so every touched entry reaches a terminal state.
				// Returning here would strand those entries in statusScheduled with no producer,
				// hanging all future readers of those keys.
				firstErr = outcome.err
			}
		case outcome.immediate:
			// Resolved from cache. A not-found (deleted) key counts as a hit but is not a result.
			if outcome.found {
				results[keyStr] = outcome.value
			}
			hits++
		default:
			pending = append(pending, pendingRead{
				key:           keyStr,
				entry:         outcome.entry,
				valueChan:     outcome.valueChan,
				needsSchedule: outcome.needsSchedule,
			})
		}
	}
	s.lock.Unlock()

	if hits > 0 {
		s.metrics.reportCacheHits(hits)
	}

	if err := s.cache.resolveBatch(pending, results, firstErr); err != nil {
		// DB errors are fatal; fail the whole batch.
		return nil, err
	}
	return results, nil
}

// getSizeInfo returns the current cache size (bytes) and entry count under the shard lock.
func (s *shard) getSizeInfo() (bytes uint64, entries uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.cache.sizeInfoLocked()
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

// Commit seals the current version; all future updates will be applied to the next version.
// The value returned is the new version number (for sanity checking).
func (s *shard) Commit() uint64 {
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
	s.cache.putRetiredLocked(combinedData)

	// Update the oldest version.
	s.oldestVersion = lastVersion

	return nil
}
