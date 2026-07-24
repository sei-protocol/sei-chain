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
)

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

// readCache is a read-through cache over the backing DB with permanent failure latching. It
// knows nothing about versions or snapshots; the shard resolves versioned data first and
// consults the cache only for keys with no in-memory override.
//
// The cache is a passive component of its shard and shares the shard's mutex: it holds no lock
// of its own. Methods with the Locked postfix require the shared lock to be held and never
// block; resolve and resolveBatch run without the lock and may block on DB reads; the
// background read-completion paths (injectValue, bulkInjectValues) acquire the lock themselves
// for a single self-contained section each. Keeping one mutex preserves the engine's
// single-lock-grab read path.
type readCache struct {
	// Cancelled when the engine shuts down; interrupts blocked waits on in-flight reads.
	ctx context.Context

	// The underlying key-value database.
	db types.KeyValueDB

	// A pool for asynchronous reads.
	readPool threading.Pool

	// The shard's mutex, shared with this cache (see the type doc).
	lock *sync.Mutex

	// Maps the context cancellation observed by a blocked read to the engine's shutdown error:
	// the latched fatal error, or ErrEngineClosed on a clean close. Blocked reads select on ctx,
	// which is cancelled only when the engine shuts down, and the Close contract requires the
	// error released to such callers to wrap ErrEngineClosed or the fatal error.
	shutdownError func() error

	// SnapshotEngine-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *SnapshotEngineMetrics

	// The estimated bookkeeping overhead per entry, in bytes, counted toward the size budget.
	overheadPerEntry uint64

	// The maximum size of the cache, in bytes.
	maxSize uint64

	// The cached entries, keyed by string(key).
	entries map[string]*cacheEntry

	// Organizes entries for LRU eviction. Only entries in a terminal data state
	// (available/deleted) are in the queue; scheduled and failed entries are deliberately
	// unevictable (see statusFailed).
	gcQueue *structures.LRUQueue
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

// A single entry in the cache. Records data for a single key.
type cacheEntry struct {
	// The parent cache that contains this entry.
	cache *readCache

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

// Tracks a key whose value is not yet available and must be waited on.
type pendingRead struct {
	key           string
	entry         *cacheEntry
	valueChan     chan readResult
	needsSchedule bool
	// Populated after the read completes, used by bulkInjectValues.
	result readResult
}

// lookupOutcome is the result of classifying a single read under the lock: either an immediate
// terminal result, or a wait plan that resolve completes outside the lock.
type lookupOutcome struct {
	// True when the read was resolved from the cache without waiting; value/found/err below
	// carry the result.
	immediate bool

	// The value, when immediate and found.
	value []byte

	// Whether the key was found, when immediate.
	found bool

	// The latched failure for this key, when immediate. Nil otherwise.
	err error

	// The channel carrying the read result, when not immediate.
	valueChan chan readResult

	// The entry being waited on, when not immediate.
	entry *cacheEntry

	// True when this caller performed the unknown -> scheduled transition and must therefore
	// submit the DB read; false when another goroutine's read is already in flight.
	needsSchedule bool
}

// newReadCache creates a readCache sharing the given mutex (see the type doc for the locking
// contract).
func newReadCache(
	ctx context.Context,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for asynchronous reads.
	readPool threading.Pool,
	// The shard's mutex, shared with this cache.
	lock *sync.Mutex,
	// The maximum size of the cache, in bytes.
	maxSize uint64,
	// The estimated bookkeeping overhead per entry, in bytes.
	overheadPerEntry uint64,
	// Maps the context cancellation observed by a blocked read to the engine's shutdown error.
	shutdownError func() error,
) *readCache {
	return &readCache{
		ctx:              ctx,
		db:               db,
		readPool:         readPool,
		lock:             lock,
		shutdownError:    shutdownError,
		overheadPerEntry: overheadPerEntry,
		maxSize:          maxSize,
		entries:          make(map[string]*cacheEntry),
		gcQueue:          structures.NewLRUQueue(),
	}
}

// readFromDB reads a single key from the underlying database, returning (nil, false, nil) if the
// key is not found and reserving errors for actual failures (e.g. I/O errors).
func (c *readCache) readFromDB(key []byte) (value []byte, found bool, err error) {
	val, err := c.db.Get(key)
	if err != nil {
		if errors.Is(err, errorutils.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read value from database: %w", err)
	}
	return val, true, nil
}

// lookupLocked classifies a read of the given key and returns how to complete it: either an
// immediate terminal result, or a wait plan for resolve. Pure state transition: it never blocks,
// and it performs the unknown -> scheduled transition under the lock, so a given read is
// scheduled by exactly one caller.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) lookupLocked(
	// The key to classify.
	key []byte,
	// If true, a cache hit moves the entry to the back of the LRU queue. False is useful when an
	// operation is performed multiple times in close succession on the same key, since the update
	// has non-zero overhead and little benefit in that case.
	updateLru bool,
) lookupOutcome {
	entry := c.entryLocked(key, true)

	switch entry.status {
	case statusAvailable:
		if updateLru {
			c.gcQueue.Touch(key)
		}
		return lookupOutcome{immediate: true, value: entry.value, found: true}
	case statusDeleted:
		if updateLru {
			c.gcQueue.Touch(key)
		}
		return lookupOutcome{immediate: true}
	case statusFailed:
		// The failure is permanent (see statusFailed).
		return lookupOutcome{
			immediate: true,
			err:       fmt.Errorf("an earlier read of this key failed: %w", entry.err),
		}
	case statusScheduled:
		return lookupOutcome{valueChan: entry.valueChan, entry: entry}
	case statusUnknown:
		entry.status = statusScheduled
		entry.valueChan = make(chan readResult, 1)
		return lookupOutcome{valueChan: entry.valueChan, entry: entry, needsSchedule: true}
	default:
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// resolve completes a read classified by lookupLocked. Must be called without the shared lock:
// it submits the DB read when this caller owns scheduling, and may block until the in-flight
// read completes.
func (c *readCache) resolve(key []byte, outcome lookupOutcome) ([]byte, bool, error) {
	if outcome.immediate {
		if outcome.err != nil {
			return nil, false, outcome.err
		}
		c.metrics.reportCacheHits(1)
		return outcome.value, outcome.found, nil
	}

	c.metrics.reportCacheMisses(1)
	startTime := time.Now()

	if outcome.needsSchedule {
		entry := outcome.entry
		c.readPool.Submit(func() {
			value, _, readErr := c.readFromDB(key)
			entry.injectValue(key, readResult{value: value, err: readErr})
		})
	}

	result, err := threading.InterruptiblePull(c.ctx, outcome.valueChan)
	c.metrics.reportCacheMissLatency(time.Since(startTime))
	if err != nil {
		// The pull is interrupted only by ctx cancellation, which means the engine is shutting
		// down; report the engine's shutdown error per the Close contract.
		return nil, false, fmt.Errorf("engine shut down while awaiting read: %w", c.shutdownError())
	}
	outcome.valueChan <- result // reload the channel in case there are other listeners
	if result.err != nil {
		return nil, false, fmt.Errorf("scheduled read failed: %w", result.err)
	}
	return result.value, result.value != nil, nil
}

// resolveBatch completes the pending reads of a batch classified via lookupLocked, writing found
// values into results. Must be called without the shared lock: it schedules the not-yet-scheduled
// reads and blocks until every pending read completes, then applies the terminal cache states
// asynchronously (bulkInjectValues).
//
// firstErr carries the caller's classification error (a latched statusFailed key), preserving
// error precedence: it is returned after the full drain unless the engine shuts down first.
// A non-nil return means the whole batch failed.
func (c *readCache) resolveBatch(pending []pendingRead, results map[string][]byte, firstErr error) error {
	if len(pending) == 0 {
		return firstErr
	}

	c.metrics.reportCacheMisses(int64(len(pending)))
	startTime := time.Now()

	for i := range pending {
		if pending[i].needsSchedule {
			p := &pending[i]
			c.readPool.Submit(func() {
				value, _, readErr := c.readFromDB([]byte(p.key))
				p.entry.valueChan <- readResult{value: value, err: readErr}
			})
		}
	}

	// Drain every pending read even if one fails: each scheduled read pushes exactly one token,
	// so the drain is bounded by reads already in flight, and it leaves every entry in a terminal
	// state (available/deleted/failed) via bulkInjectValues below. Abandoning the drain on the
	// first error would strand the remaining entries in statusScheduled with unpopulated results.
	for i := range pending {
		result, err := threading.InterruptiblePull(c.ctx, pending[i].valueChan)
		if err != nil {
			// Context cancellation is a hard teardown: post-shutdown entry state is
			// unobservable, and draining could block on reads that never complete while the
			// pool is being torn down, so bail immediately with the engine's shutdown error
			// per the Close contract.
			return fmt.Errorf("engine shut down while awaiting batch read: %w", c.shutdownError())
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

	c.metrics.reportCacheMissLatency(time.Since(startTime))
	go c.bulkInjectValues(pending)

	return firstErr
}

// This method is called by the read scheduler when a value becomes available.
func (e *cacheEntry) injectValue(key []byte, result readResult) {
	c := e.cache
	c.lock.Lock()

	if e.status == statusScheduled {
		if result.err != nil {
			// Latch the failure permanently (see statusFailed): the error has been propagated
			// to a caller, and a later retry that succeeded would be a fork risk. The entry is
			// deliberately kept out of the LRU queue so it can never be evicted and re-read.
			e.status = statusFailed
			e.err = result.err
		} else if result.value == nil {
			e.status = statusDeleted
			e.value = nil
			size := uint64(len(key)) + c.overheadPerEntry
			c.gcQueue.Push(key, size)
			c.evictLocked()
		} else {
			e.status = statusAvailable
			e.value = result.value
			size := uint64(len(key)) + uint64(len(result.value)) + c.overheadPerEntry
			c.gcQueue.Push(key, size)
			c.evictLocked()
		}
	}

	c.lock.Unlock()

	e.valueChan <- result
}

// Applies deferred cache updates for a batch of reads under a single lock acquisition.
func (c *readCache) bulkInjectValues(reads []pendingRead) {
	c.lock.Lock()
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
			size := uint64(len(reads[i].key)) + c.overheadPerEntry
			c.gcQueue.Push([]byte(reads[i].key), size)
		} else {
			entry.status = statusAvailable
			entry.value = result.value
			size := uint64(len(reads[i].key)) + uint64(len(result.value)) + c.overheadPerEntry
			c.gcQueue.Push([]byte(reads[i].key), size)
		}
	}
	c.evictLocked()
	c.lock.Unlock()
}

// Get a cache entry for a given key.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) entryLocked(key []byte, createIfMissing bool) *cacheEntry {
	if entry, ok := c.entries[string(key)]; ok {
		return entry
	}
	if !createIfMissing {
		return nil
	}
	entry := &cacheEntry{
		cache:  c,
		status: statusUnknown,
	}
	c.entries[string(key)] = entry
	return entry
}

// putRetiredLocked installs data retired out of the shard's MVCC layer. A nil value marks the
// key as known-deleted (the engine-wide tombstone convention); any other value is cached as
// available. Inserts everything, then evicts overflow once at the end.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) putRetiredLocked(data map[string][]byte) {
	for k, v := range data {
		if v == nil {
			c.deleteRetiredLocked([]byte(k))
		} else {
			c.setRetiredLocked([]byte(k), v)
		}
	}

	// These insertions may have caused the cache to exceed its size budget, do necessary
	// evictions. setRetiredLocked does not evict on its own, so this is the enforcement point
	// for the bulk insert above.
	c.evictLocked()
}

// Set a retired value.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) setRetiredLocked(key []byte, value []byte) {
	entry := c.entryLocked(key, true)
	entry.status = statusAvailable
	entry.value = value
	// Overwriting a failed entry is deliberate: this data comes from the engine's own retired
	// writes, not from the failed DB read, and the original error has already been propagated.
	entry.err = nil

	size := uint64(len(key)) + uint64(len(value)) + c.overheadPerEntry
	c.gcQueue.Push(key, size)
}

// Delete a retired value.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) deleteRetiredLocked(key []byte) {
	entry := c.entryLocked(key, false)
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

	size := uint64(len(key)) + c.overheadPerEntry
	c.gcQueue.Push(key, size)
}

// Evicts least recently used entries until the cache is within its size budget.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) evictLocked() {
	for c.gcQueue.GetTotalSize() > c.maxSize {
		next := c.gcQueue.PopLeastRecentlyUsed()
		delete(c.entries, next)
	}
}

// sizeInfoLocked returns the current size (bytes) and entry count.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) sizeInfoLocked() (bytes uint64, entries uint64) {
	return c.gcQueue.GetTotalSize(), c.gcQueue.GetCount()
}
