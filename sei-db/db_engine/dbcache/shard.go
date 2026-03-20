package dbcache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// A single shard of a Cache.
type shard struct {
	ctx    context.Context
	config *CacheConfig

	// A method that can read data from the DB.
	reader Reader

	// A lock to protect the shard's data.
	lock sync.Mutex

	// Data at various versions. This is for data that has not yet been flushed down into the DB.
	versionedData map[string] /* key */ *Deque[versionedValue] /* values at various versions */

	// For each version, contains the values set in that version.  If a value is set more than once
	// in a version, only the last value is stored. Although possible to find the value at a specifc
	// version by iterating over this map, it is much more efficient to use the versionedData map.
	versionDiffs map[uint64] /* version */ map[string] /* key */ []byte /* value */

	// A cache containing data as it is in the DB.
	dbCache map[string]*dbCacheEntry

	// Organizes data in dbCache for LRU eviction.
	dbCacheGCQueue *lruQueue

	// A pool for asynchronous reads.
	readPool threading.Pool

	// The maximum size of the db cache, in bytes.
	maxSize uint64

	// Cache-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *CacheMetrics

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
	statusUnknown valueStatus = iota
	// We've scheduled a read of the value but haven't yet finished the read.
	statusScheduled
	// The data is available.
	statusAvailable
	// We are aware that the value is deleted (special case of data being available).
	statusDeleted
)

// A single entry in the db cache. Records data for a single key.
type dbCacheEntry struct {
	// The parent shard that contains this entry.
	shard *shard

	// The current status of this entry.
	status valueStatus

	// The value, if known.
	value []byte

	// If the value is not available when we request it,
	// it will be written to this channel when it is available.
	valueChan chan readResult
}

// A single value at a specific version.
type versionedValue struct {
	// The value.
	value []byte
	// The version number when this value was written to the DB. Note that this is NOT the same
	// as block height, this is just a version number that montotically increases over the lifetime
	// of a cache instance.
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
	config *CacheConfig,
	// A method that can read data from the DB.
	reader Reader,
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
		reader:         reader,
		readPool:       readPool,
		lock:           sync.Mutex{},
		dbCache:        make(map[string]*dbCacheEntry),
		dbCacheGCQueue: newLRUQueue(),
		maxSize:        maxSize,
		versionedData:  make(map[string]*Deque[versionedValue]),
		versionDiffs:   versionDiffs,
		currentVersion: 1, // important: versions start at 1, not 0, to allow version-1 without underflow
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

	if err := s.validateVersionUnlocked(version); err != nil {
		s.lock.Unlock()
		return nil, false, err
	}

	// First, check to see if we have this value in the versioned data map.
	if value, found := s.lookupVersionedUnlocked(string(key), version); found {
		s.lock.Unlock()
		return value, value != nil, nil
	}

	// If we reach this point, we didn't find a value for this version in the versioned data map.
	// Check the DB cache, and potentially read from the DB if it's not in memory.

	entry := s.getDBCacheEntry(key, true)

	switch entry.status {
	case statusAvailable:
		return s.getAvailable(entry, key, updateLru)
	case statusDeleted:
		return s.getDeleted(key, updateLru)
	case statusScheduled:
		return s.getScheduled(entry)
	case statusUnknown:
		return s.getUnknown(entry, key)
	default:
		s.lock.Unlock()
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// Handles Get for a key whose value is already cached. Lock must be held; releases it.
func (s *shard) getAvailable(entry *dbCacheEntry, key []byte, updateLru bool) ([]byte, bool, error) {
	value := entry.value
	if updateLru {
		s.dbCacheGCQueue.Touch(key)
	}
	s.lock.Unlock()
	s.metrics.reportCacheHits(1)
	return value, true, nil
}

// Handles Get for a key known to be deleted. Lock must be held; releases it.
func (s *shard) getDeleted(key []byte, updateLru bool) ([]byte, bool, error) {
	if updateLru {
		s.dbCacheGCQueue.Touch(key)
	}
	s.lock.Unlock()
	s.metrics.reportCacheHits(1)
	return nil, false, nil
}

// Handles Get for a key with an in-flight read from another goroutine. Lock must be held; releases it.
func (s *shard) getScheduled(entry *dbCacheEntry) ([]byte, bool, error) {
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
		return nil, false, fmt.Errorf("failed to read value from database: %w", result.err)
	}
	return result.value, result.value != nil, nil
}

// Handles Get for a key not yet read. Schedules the read and waits. Lock must be held; releases it.
func (s *shard) getUnknown(entry *dbCacheEntry, key []byte) ([]byte, bool, error) {
	entry.status = statusScheduled
	valueChan := make(chan readResult, 1)
	entry.valueChan = valueChan
	s.lock.Unlock()
	s.metrics.reportCacheMisses(1)
	startTime := time.Now()
	err := s.readPool.Submit(s.ctx, func() {
		value, _, readErr := s.reader(key)
		entry.injectValue(key, readResult{value: value, err: readErr})
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to schedule read: %w", err)
	}
	result, err := threading.InterruptiblePull(s.ctx, valueChan)
	s.metrics.reportCacheMissLatency(time.Since(startTime))
	if err != nil {
		return nil, false, fmt.Errorf("failed to pull value from channel: %w", err)
	}
	valueChan <- result // reload the channel in case there are other listeners
	if result.err != nil {
		return nil, false, result.err
	}
	return result.value, result.value != nil, nil
}

// This method is called by the read scheduler when a value becomes available.
func (se *dbCacheEntry) injectValue(key []byte, result readResult) {
	se.shard.lock.Lock()

	if se.status == statusScheduled {
		if result.err != nil {
			// Don't cache errors — reset so the next caller retries.
			delete(se.shard.dbCache, string(key))
		} else if result.value == nil {
			se.status = statusDeleted
			se.value = nil
			size := uint64(len(key)) + se.shard.config.EstimatedOverheadPerEntry
			se.shard.dbCacheGCQueue.Push(key, size)
			se.shard.evictUnlocked()
		} else {
			se.status = statusAvailable
			se.value = result.value
			size := uint64(len(key)) + uint64(len(result.value)) + se.shard.config.EstimatedOverheadPerEntry
			se.shard.dbCacheGCQueue.Push(key, size)
			se.shard.evictUnlocked()
		}
	}

	se.shard.lock.Unlock()

	se.valueChan <- result
}

// Get a cb cache entry for a given key. Caller is responsible for holding the shard's lock
// when this method is called.
func (s *shard) getDBCacheEntry(key []byte, createIfMissing bool) *dbCacheEntry {
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

// validateVersionUnlocked checks that the given version is within the valid range.
// Caller must hold the lock.
func (s *shard) validateVersionUnlocked(version uint64) error {
	if version < s.oldestVersion {
		return fmt.Errorf("version (%d) is less than the oldest version (%d)", version, s.oldestVersion)
	}
	if version > s.currentVersion {
		return fmt.Errorf("version (%d) is greater than the current version (%d)", version, s.currentVersion)
	}
	return nil
}

// lookupVersionedUnlocked checks versioned data for a key at the given version.
// Returns (value, true) if found in versioned data, (nil, false) if the dbCache should be consulted.
// Caller must hold the lock.
func (s *shard) lookupVersionedUnlocked(key string, version uint64) ([]byte, bool) {
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
func (s *shard) BatchGet(
	// A map containing the keys to read. Values are written to this map as they are read.
	keys map[string]types.BatchGetResult,
	// The version of the data to get.
	version uint64,
) error {
	pending := make([]pendingRead, 0, len(keys))
	var hits int64

	s.lock.Lock()

	if err := s.validateVersionUnlocked(version); err != nil {
		s.lock.Unlock()
		return err
	}

	for key := range keys {
		if value, found := s.lookupVersionedUnlocked(key, version); found {
			keys[key] = types.BatchGetResult{Value: value}
			hits++
			continue
		}

		entry := s.getDBCacheEntry([]byte(key), true)

		switch entry.status {
		case statusAvailable, statusDeleted:
			keys[key] = types.BatchGetResult{Value: entry.value}
			hits++
		case statusScheduled:
			pending = append(pending, pendingRead{
				key:       key,
				entry:     entry,
				valueChan: entry.valueChan,
			})
		case statusUnknown:
			entry.status = statusScheduled
			valueChan := make(chan readResult, 1)
			entry.valueChan = valueChan
			pending = append(pending, pendingRead{
				key:           key,
				entry:         entry,
				valueChan:     valueChan,
				needsSchedule: true,
			})
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
		return nil
	}

	s.metrics.reportCacheMisses(int64(len(pending)))
	startTime := time.Now()

	for i := range pending {
		if pending[i].needsSchedule {
			p := &pending[i]
			err := s.readPool.Submit(s.ctx, func() {
				value, _, readErr := s.reader([]byte(p.key))
				p.entry.valueChan <- readResult{value: value, err: readErr}
			})
			if err != nil {
				return fmt.Errorf("failed to schedule read: %w", err)
			}
		}
	}

	for i := range pending {
		result, err := threading.InterruptiblePull(s.ctx, pending[i].valueChan)
		if err != nil {
			return fmt.Errorf("failed to pull value from channel: %w", err)
		}
		pending[i].valueChan <- result
		pending[i].result = result

		if result.err != nil {
			keys[pending[i].key] = types.BatchGetResult{Error: result.err}
		} else {
			keys[pending[i].key] = types.BatchGetResult{Value: result.value}
		}
	}

	s.metrics.reportCacheMissLatency(time.Since(startTime))
	go s.bulkInjectValues(pending)

	return nil
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
			// Don't cache errors — reset so the next caller retries.
			delete(s.dbCache, reads[i].key)
		} else if result.value == nil {
			entry.status = statusDeleted
			entry.value = nil
			size := uint64(len(reads[i].key)) + s.config.EstimatedOverheadPerEntry
			s.dbCacheGCQueue.Push([]byte(reads[i].key), size)
		} else {
			entry.status = statusAvailable
			entry.value = result.value
			size := uint64(len(reads[i].key)) + uint64(len(result.value)) + s.config.EstimatedOverheadPerEntry
			s.dbCacheGCQueue.Push([]byte(reads[i].key), size)
		}
	}
	s.evictUnlocked()
	s.lock.Unlock()
}

// Evicts least recently used entries until the cache is within its size budget.
// Caller is required to hold the lock.
func (s *shard) evictUnlocked() {
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
	s.setUnlocked(key, value)
	s.lock.Unlock()
}

// setUnlocked writes a value to the versioned data structures at the current version.
// Caller must hold the lock.
func (s *shard) setUnlocked(key []byte, value []byte) {
	keyStr := string(key)
	s.versionDiffs[s.currentVersion][keyStr] = value

	deque, ok := s.versionedData[keyStr]
	if !ok {
		deque = NewDeque[versionedValue]()
		s.versionedData[keyStr] = deque
	}
	if deque.IsEmpty() || deque.PeekBack().version < s.currentVersion {
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	} else {
		deque.PopBack()
		deque.PushBack(versionedValue{version: s.currentVersion, value: value})
	}
}

// Set a value. Caller is required to hold the lock.
func (s *shard) setInDBCacheUnlocked(key []byte, value []byte) {
	entry := s.getDBCacheEntry(key, true)
	entry.status = statusAvailable
	entry.value = value

	size := uint64(len(key)) + uint64(len(value)) + s.config.EstimatedOverheadPerEntry
	s.dbCacheGCQueue.Push(key, size)
}

// BatchSet sets the values for a batch of keys at the current version.
func (s *shard) BatchSet(entries []CacheUpdate) {
	s.lock.Lock()
	for i := range entries {
		s.setUnlocked(entries[i].Key, entries[i].Value)
	}
	s.lock.Unlock()
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) {
	s.Set(key, nil)
}

// Delete a value. Caller is required to hold the lock.
func (s *shard) deleteInDBCacheUnlocked(key []byte) {
	entry := s.getDBCacheEntry(key, false)
	if entry == nil {
		// Key is not in the cache, so nothing to do.
		return
	}
	entry.status = statusDeleted
	entry.value = nil

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

// Get the diffs for a range of versions. The returned data should not be mutated in any way, but are otherwise
// thread safe to read.
func (s *shard) GetDiffsForVersions(firstVersion uint64, lastVersion uint64) ([]map[string][]byte, error) {

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
	if lastVersion >= s.currentVersion {
		return nil, fmt.Errorf("lastVersion (%d) must be less than the current version (%d)",
			lastVersion, s.currentVersion)
	}

	diffs := make([]map[string][]byte, 0, lastVersion-firstVersion+1)
	for v := firstVersion; v <= lastVersion; v++ {
		diffs = append(diffs, s.versionDiffs[v])
	}
	return diffs, nil
}

// Drop versions, pushing their data down into the DB cache. The first version to drop must be equal to the
// oldest version currently being tracked.
func (s *shard) DropVersions(firstVersion uint64, lastVersion uint64) error {

	if firstVersion > lastVersion {
		return fmt.Errorf("firstVersion (%d) must be less than or equal to lastVersion (%d)",
			firstVersion, lastVersion)
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	if firstVersion != s.oldestVersion {
		return fmt.Errorf("firstVersion (%d) must be equal to the oldest version (%d)",
			firstVersion, s.oldestVersion)
	}
	if lastVersion >= s.currentVersion {
		return fmt.Errorf("lastVersion (%d) must be less than the current version (%d)",
			lastVersion, s.currentVersion)
	}

	// Combine the data from all versions being dropped.
	var combinedData map[string][]byte
	if firstVersion == lastVersion {
		// single version
		combinedData = s.versionDiffs[firstVersion]
	} else {
		// range of versions
		combinedData = make(map[string][]byte)
		for version := firstVersion; version <= lastVersion; version++ {
			for k, value := range s.versionDiffs[version] {
				combinedData[k] = value
			}
		}
	}

	// Drop the version diffs that we will no longer need.
	for v := firstVersion; v <= lastVersion; v++ {
		delete(s.versionDiffs, v)
	}

	// Clean up the versioned data map.
	for k := range combinedData {
		deque := s.versionedData[k]
		for !deque.IsEmpty() {
			next := deque.PeekFront()
			if next.version > lastVersion {
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
			s.deleteInDBCacheUnlocked([]byte(k))
		} else {
			s.setInDBCacheUnlocked([]byte(k), v)
		}
	}

	// Cache insertions may have caused the cache to exceed its size budget, do necessary evictions.
	s.evictUnlocked()

	// Update the oldset version.
	s.oldestVersion = lastVersion + 1

	return nil
}
