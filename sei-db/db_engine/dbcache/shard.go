package dbcache

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// A single shard of a Cache.
type shard struct {
	ctx context.Context

	// A lock to protect the shard's data.
	lock sync.Mutex

	// The data in the shard.
	data map[string]*shardEntry

	// Organizes data for garbage collection.
	gcQueue *lruQueue

	// A pool for asynchronous reads.
	readPool threading.Pool

	// The maximum size of this cache, in bytes.
	maxSize uint64

	// Cache-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *CacheMetrics
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
	// We've scheduled a read of the value but haven't yet finsihed the read.
	statusScheduled
	// The data is available.
	statusAvailable
	// We are aware that the value is deleted (special case of data being available).
	statusDeleted
)

// A single shardEntry in a shard. Records data for a single key.
type shardEntry struct {
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

// Creates a new Shard.
func NewShard(
	ctx context.Context,
	readPool threading.Pool,
	maxSize uint64,
) (*shard, error) {

	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	return &shard{
		ctx:      ctx,
		readPool: readPool,
		lock:     sync.Mutex{},
		data:     make(map[string]*shardEntry),
		gcQueue:  newLRUQueue(),
		maxSize:  maxSize,
	}, nil
}

// Get returns the value for the given key, or (nil, false, nil) if not found.
func (s *shard) Get(read Reader, key []byte, updateLru bool) ([]byte, bool, error) {
	s.lock.Lock()

	entry := s.getEntry(key)

	switch entry.status {
	case statusAvailable:
		return s.getAvailable(entry, key, updateLru)
	case statusDeleted:
		return s.getDeleted(key, updateLru)
	case statusScheduled:
		return s.getScheduled(entry)
	case statusUnknown:
		return s.getUnknown(read, entry, key)
	default:
		s.lock.Unlock()
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// Handles Get for a key whose value is already cached. Lock must be held; releases it.
func (s *shard) getAvailable(entry *shardEntry, key []byte, updateLru bool) ([]byte, bool, error) {
	value := bytes.Clone(entry.value)
	if updateLru {
		s.gcQueue.Touch(key)
	}
	s.lock.Unlock()
	s.metrics.reportCacheHits(1)
	return value, true, nil
}

// Handles Get for a key known to be deleted. Lock must be held; releases it.
func (s *shard) getDeleted(key []byte, updateLru bool) ([]byte, bool, error) {
	if updateLru {
		s.gcQueue.Touch(key)
	}
	s.lock.Unlock()
	s.metrics.reportCacheHits(1)
	return nil, false, nil
}

// Handles Get for a key with an in-flight read from another goroutine. Lock must be held; releases it.
func (s *shard) getScheduled(entry *shardEntry) ([]byte, bool, error) {
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
func (s *shard) getUnknown(read Reader, entry *shardEntry, key []byte) ([]byte, bool, error) {
	entry.status = statusScheduled
	valueChan := make(chan readResult, 1)
	entry.valueChan = valueChan
	s.lock.Unlock()
	s.metrics.reportCacheMisses(1)
	startTime := time.Now()
	err := s.readPool.Submit(s.ctx, func() {
		value, _, readErr := read(key)
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
func (se *shardEntry) injectValue(key []byte, result readResult) {
	se.shard.lock.Lock()

	if se.status == statusScheduled {
		if result.err != nil {
			// Don't cache errors — reset so the next caller retries.
			delete(se.shard.data, string(key))
		} else if result.value == nil {
			se.status = statusDeleted
			se.value = nil
			se.shard.gcQueue.Push(key, uint64(len(key)))
			se.shard.evictUnlocked()
		} else {
			se.status = statusAvailable
			se.value = result.value
			se.shard.gcQueue.Push(key, uint64(len(key)+len(result.value))) //nolint:gosec // G115: len is non-negative
			se.shard.evictUnlocked()
		}
	}

	se.shard.lock.Unlock()

	se.valueChan <- result
}

// Get a shard entry for a given key. Caller is responsible for holding the shard's lock
// when this method is called.
func (s *shard) getEntry(key []byte) *shardEntry {
	if entry, ok := s.data[string(key)]; ok {
		return entry
	}
	entry := &shardEntry{
		shard:  s,
		status: statusUnknown,
	}
	keyStr := string(key)
	s.data[keyStr] = entry
	return entry
}

// Tracks a key whose value is not yet available and must be waited on.
type pendingRead struct {
	key           string
	entry         *shardEntry
	valueChan     chan readResult
	needsSchedule bool
	// Populated after the read completes, used by bulkInjectValues.
	result readResult
}

// BatchGet reads a batch of keys from the shard. Results are written into the provided map.
func (s *shard) BatchGet(read Reader, keys map[string]types.BatchGetResult) error {
	pending := make([]pendingRead, 0, len(keys))
	var hits int64

	s.lock.Lock()
	for key := range keys {
		entry := s.getEntry([]byte(key))

		switch entry.status {
		case statusAvailable, statusDeleted:
			keys[key] = types.BatchGetResult{Value: bytes.Clone(entry.value)}
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
				value, _, readErr := read([]byte(p.key))
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
			delete(s.data, reads[i].key)
		} else if result.value == nil {
			entry.status = statusDeleted
			entry.value = nil
			s.gcQueue.Push([]byte(reads[i].key), uint64(len(reads[i].key)))
		} else {
			entry.status = statusAvailable
			entry.value = result.value
			s.gcQueue.Push([]byte(reads[i].key), uint64(len(reads[i].key)+len(result.value))) //nolint:gosec // G115
		}
	}
	s.evictUnlocked()
	s.lock.Unlock()
}

// Evicts least recently used entries until the cache is within its size budget.
// Caller is required to hold the lock.
func (s *shard) evictUnlocked() {
	for s.gcQueue.GetTotalSize() > s.maxSize {
		next := s.gcQueue.PopLeastRecentlyUsed()
		delete(s.data, next)
	}
}

// getSizeInfo returns the current size (bytes) and entry count under the shard lock.
func (s *shard) getSizeInfo() (bytes uint64, entries uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.gcQueue.GetTotalSize(), s.gcQueue.GetCount()
}

// Set sets the value for the given key.
func (s *shard) Set(key []byte, value []byte) {
	s.lock.Lock()
	s.setUnlocked(key, value)
	s.lock.Unlock()
}

// Set a value. Caller is required to hold the lock.
func (s *shard) setUnlocked(key []byte, value []byte) {
	entry := s.getEntry(key)
	entry.status = statusAvailable
	entry.value = value

	s.gcQueue.Push(key, uint64(len(key)+len(value))) //nolint:gosec // G115
	s.evictUnlocked()
}

// BatchSet sets the values for a batch of keys.
func (s *shard) BatchSet(entries []CacheUpdate) {
	s.lock.Lock()
	for i := range entries {
		if entries[i].IsDelete() {
			s.deleteUnlocked(entries[i].Key)
		} else {
			s.setUnlocked(entries[i].Key, entries[i].Value)
		}
	}
	s.lock.Unlock()
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) {
	s.lock.Lock()
	s.deleteUnlocked(key)
	s.lock.Unlock()
}

// Delete a value. Caller is required to hold the lock.
func (s *shard) deleteUnlocked(key []byte) {
	entry := s.getEntry(key)
	entry.status = statusDeleted
	entry.value = nil

	s.gcQueue.Push(key, uint64(len(key)))
	s.evictUnlocked()
}
