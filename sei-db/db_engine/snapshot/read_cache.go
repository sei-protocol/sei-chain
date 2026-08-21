package snapshot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// readCache is a read-through cache over the backing DB. It knows nothing about versions or
// snapshots; the shard resolves versioned data first and consults the cache only for keys with no
// in-memory override.
//
// A failed DB read is fatal: the cache bricks the engine, which takes every shard out of service so
// no further reads are served (see outOfServiceErr).
//
// Recency is approximate, and that is what allows a read to hold only a shared lock. A true LRU has to
// be reordered on every hit, which makes a read a write and forces every reader to exclude every
// other; here a reader stamps the entry it read with the current epoch and eviction picks the oldest
// of a small sample (see evictLocked). Nothing about the eviction choice affects correctness, since a
// key evicted early is simply read from the backing DB again.
//
// The cache is a passive component of its shard and shares the shard's lock: it holds no lock of its
// own. Methods with the Locked postfix require that lock held and never block; resolve and resolveBatch
// run without it and may block on DB reads; the background read-completion paths (injectValue,
// bulkInjectValues) take it themselves for a single self-contained section each. A read that hits needs
// only shared access, while one that misses mutates the entry and needs it exclusively.
type readCache struct {
	// Cancelled when the engine shuts down; interrupts blocked waits on in-flight reads.
	ctx context.Context

	// The underlying key-value database.
	db types.KeyValueDB

	// A pool for asynchronous reads.
	readPool threading.Pool

	// The shard's lock, shared with this cache (see the type doc). A hit needs only its read side.
	lock *sync.RWMutex

	// Maps the context cancellation observed by a blocked read to the engine's shutdown error:
	// the latched fatal error, or ErrEngineClosed on a clean close. Blocked reads select on ctx,
	// which is cancelled only when the engine shuts down, and the Close contract requires the
	// error released to such callers to wrap ErrEngineClosed or the fatal error.
	shutdownError func() error

	// Reports a failed DB read to the engine, which bricks and stops serving reads. Called
	// without the shard lock held, since it acquires the engine's versionLock.
	reportReadFailure func(error)

	// The failure that took this cache out of service, or nil while it is healthy. Set when the
	// engine bricks — for any reason, not only a failed read of this shard — after which the shard
	// refuses reads rather than serving data the engine can no longer vouch for.
	//
	// Guarded by the shared lock.
	outOfServiceErr error

	// SnapshotEngine-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *SnapshotEngineMetrics

	// The estimated bookkeeping overhead per entry, in bytes, counted toward the size budget.
	overheadPerEntry uint64

	// The maximum size of the cache, in bytes.
	maxSize uint64

	// The cached entries, keyed by string(key).
	entries map[string]*cacheEntry

	// The number of bytes and entries counted toward the size budget. Only entries in a terminal data
	// state (available/deleted) are counted: scheduled entries have no value yet, and failed entries
	// have no value to serve.
	//
	// Guarded by the shared lock.
	trackedBytes uint64
	trackedCount uint64

	// Advanced once per maintenance pass, and stamped onto an entry by a reader that serves a value
	// from it. Eviction prefers entries whose stamp is oldest, so an epoch is the unit of recency.
	//
	// Guarded by the shared lock, which readers hold while stamping.
	epoch uint64
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
	// A read of this value from the DB failed. This is a terminal state so that readers already
	// waiting on the entry are never stranded; it is not the mechanism that keeps the failure from
	// being papered over. A failed read bricks the engine and takes the cache out of service (see
	// readCache.outOfServiceErr), so the entry is never consulted again.
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

	// If the value is not available when we request it,
	// it will be written to this channel when it is available.
	valueChan chan readResult

	// The epoch in which a reader last served a value from this entry. Atomic because readers stamp it
	// while holding only a shared lock, so several may stamp the same entry at once. Eviction needs
	// approximate recency only: a lost stamp costs one early eviction, never a wrong value.
	lastRead atomic.Uint64

	// This entry's contribution to trackedBytes, or zero while it holds no value. Held here so
	// eviction can debit it without consulting a second structure.
	//
	// Guarded by the shared lock.
	size uint64
}

// Tracks a key whose value is not yet available and must be waited on.
type pendingRead struct {
	key string
	// The key's position in the batch, which is where its value is written once the read completes.
	index         int
	entry         *cacheEntry
	valueChan     chan readResult
	needsSchedule bool
	// Populated after the read completes, used by bulkInjectValues.
	result readResult
}

// lookupOutcome is the result of classifying a single read under the lock: either an immediate
// terminal result, or a wait plan that resolve completes outside the lock. Classification never
// fails — a cache that has seen a read failure is out of service and the shard refuses the read
// before classifying it.
type lookupOutcome struct {
	// True when the read was resolved from the cache without waiting; value/found below carry the
	// result.
	immediate bool

	// The value, when immediate and found.
	value []byte

	// Whether the key was found, when immediate.
	found bool

	// The channel carrying the read result, when not immediate.
	valueChan chan readResult

	// The entry being waited on, when not immediate.
	entry *cacheEntry

	// True when this caller performed the unknown -> scheduled transition and must therefore
	// submit the DB read; false when another goroutine's read is already in flight.
	needsSchedule bool
}

// newReadCache creates a readCache sharing the given lock (see the type doc for the locking
// contract).
func newReadCache(
	ctx context.Context,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for asynchronous reads.
	readPool threading.Pool,
	// The shard's lock, shared with this cache.
	lock *sync.RWMutex,
	// The maximum size of the cache, in bytes.
	maxSize uint64,
	// The estimated bookkeeping overhead per entry, in bytes.
	overheadPerEntry uint64,
	// Maps the context cancellation observed by a blocked read to the engine's shutdown error.
	shutdownError func() error,
	// Reports a failed DB read to the engine, which bricks and stops serving reads.
	reportReadFailure func(error),
) *readCache {
	return &readCache{
		ctx:               ctx,
		db:                db,
		readPool:          readPool,
		lock:              lock,
		shutdownError:     shutdownError,
		reportReadFailure: reportReadFailure,
		overheadPerEntry:  overheadPerEntry,
		maxSize:           maxSize,
		entries:           make(map[string]*cacheEntry),
	}
}

// outOfServiceLocked returns a second-hand error if this cache has been taken out of service, or nil
// while it is healthy. The error is inherited from the earlier failure rather than produced by the
// caller's own operation.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) outOfServiceLocked() error {
	if c.outOfServiceErr == nil {
		return nil
	}
	return fmt.Errorf("shard is out of service: %w", c.outOfServiceErr)
}

// takeOutOfServiceLocked records the failure that stops this cache from serving reads. The first
// failure wins; later ones are dropped so the reported cause is the original one.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) takeOutOfServiceLocked(err error) {
	if c.outOfServiceErr == nil {
		c.outOfServiceErr = err
	}
}

// readFromDB reads a single key from the underlying database, returning (nil, false, nil) if the
// key is not found and reserving errors for actual failures (e.g. I/O errors).
//
// A nil value with found == true is impossible: per the types.KeyValueDB.Get contract, a found
// zero-length value is a non-nil empty slice. The read-completion paths (injectValue, resolve)
// depend on this — they treat a nil value as not-found/deleted, so a backend that returned nil
// for a stored empty value would silently turn that key into a tombstone.
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
// lookupSharedLocked classifies a read that a caller wants to serve while holding only the shared
// lock. It mutates nothing, so it can only report a hit; ok is false for every other state, and such a
// caller must retry under the exclusive lock where classifying may create an entry and schedule a read.
//
// Deliberately narrower than lookupLocked, which also handles a read already in flight. Joining one
// only reads the entry's channel and would be safe here, but keeping this to the two states that are
// unambiguously terminal is what makes it obvious that no shared-lock caller can mutate.
//
// The Locked postfix indicates that the caller must hold at least the shared lock.
func (c *readCache) lookupSharedLocked(key []byte, updateLru bool) (outcome lookupOutcome, ok bool) {
	entry := c.entryLocked(key, false)
	if entry == nil {
		return lookupOutcome{}, false
	}

	switch entry.status {
	case statusAvailable:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true, value: entry.value, found: true}, true
	case statusDeleted:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true}, true
	default:
		return lookupOutcome{}, false
	}
}

// lookupSharedStringLocked is lookupSharedLocked for a caller that already holds the key as a string.
//
// The Locked postfix indicates that the caller must hold at least the shared lock.
func (c *readCache) lookupSharedStringLocked(key string, updateLru bool) (outcome lookupOutcome, ok bool) {
	entry := c.entryLockedString(key, false)
	if entry == nil {
		return lookupOutcome{}, false
	}

	switch entry.status {
	case statusAvailable:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true, value: entry.value, found: true}, true
	case statusDeleted:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true}, true
	default:
		return lookupOutcome{}, false
	}
}

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
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true, value: entry.value, found: true}
	case statusDeleted:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true}
	case statusScheduled:
		return lookupOutcome{valueChan: entry.valueChan, entry: entry}
	case statusUnknown:
		entry.status = statusScheduled
		entry.valueChan = make(chan readResult, 1)
		return lookupOutcome{valueChan: entry.valueChan, entry: entry, needsSchedule: true}
	default:
		// statusFailed lands here, and that is intended: an entry becomes statusFailed only in the
		// same critical section that takes the cache out of service, and the shard checks that under
		// the same lock before classifying, so reaching this is an invariant violation rather than a
		// state to serve.
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// lookupStringLocked is lookupLocked for a caller that already holds the key as a string, which the
// cache retains rather than copying when the key turns out to be new.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) lookupStringLocked(key string, updateLru bool) lookupOutcome {
	entry := c.entryLockedString(key, true)

	switch entry.status {
	case statusAvailable:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true, value: entry.value, found: true}
	case statusDeleted:
		if updateLru {
			c.stampLocked(entry)
		}
		return lookupOutcome{immediate: true}
	case statusScheduled:
		return lookupOutcome{valueChan: entry.valueChan, entry: entry}
	case statusUnknown:
		entry.status = statusScheduled
		entry.valueChan = make(chan readResult, 1)
		return lookupOutcome{valueChan: entry.valueChan, entry: entry, needsSchedule: true}
	default:
		// statusFailed lands here, and that is intended: an entry becomes statusFailed only in the
		// same critical section that takes the cache out of service, and the shard checks that under
		// the same lock before classifying, so reaching this is an invariant violation rather than a
		// state to serve.
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// resolve completes a read classified by lookupLocked. Must be called without the shared lock:
// it submits the DB read when this caller owns scheduling, and may block until the in-flight
// read completes.
func (c *readCache) resolve(key []byte, outcome lookupOutcome) ([]byte, bool, error) {
	if outcome.immediate {
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

// resolveBatch completes the pending reads of a batch classified via lookupLocked, writing each
// read's value into values at that read's own index. Must be called without the shared lock: it
// schedules the not-yet-scheduled reads and blocks until every pending read completes, then applies
// the terminal cache states asynchronously (bulkInjectValues).
//
// A non-nil return means the whole batch failed. The first read error is returned after the full
// drain, unless the engine shuts down first.
func (c *readCache) resolveBatch(pending []pendingRead, values [][]byte) error {
	if len(pending) == 0 {
		return nil
	}

	var firstErr error

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
		values[pending[i].index] = result.value
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
			// Terminal state so readers already waiting on this entry are not stranded. The engine
			// is bricked below, so the entry is never consulted again — the error reaches the waiter
			// over valueChan, not from the entry.
			e.status = statusFailed
		} else if result.value == nil {
			e.status = statusDeleted
			e.value = nil
			c.trackLocked(e, uint64(len(key))+c.overheadPerEntry)
			c.evictLocked(c.hardCapLocked())
		} else {
			e.status = statusAvailable
			e.value = result.value
			c.trackLocked(e, uint64(len(key))+uint64(len(result.value))+c.overheadPerEntry)
			c.evictLocked(c.hardCapLocked())
		}
	}

	// Take the cache out of service regardless of the entry's status: the DB read failed, which is
	// fatal whether or not this entry was still the one waiting on it.
	if result.err != nil {
		c.takeOutOfServiceLocked(result.err)
	}

	c.lock.Unlock()

	// Release the waiter before bricking. reportReadFailure acquires the engine's versionLock, and
	// nobody may be blocked on us while we wait for it.
	e.valueChan <- result

	if result.err != nil {
		c.reportReadFailure(result.err)
	}
}

// Applies deferred cache updates for a batch of reads under a single lock acquisition.
func (c *readCache) bulkInjectValues(reads []pendingRead) {
	c.lock.Lock()
	var failure error
	for i := range reads {
		// Recorded before the status check below: a failed DB read is fatal whether or not this
		// entry was still the one waiting on it.
		if reads[i].result.err != nil && failure == nil {
			failure = reads[i].result.err
		}

		entry := reads[i].entry
		if entry.status != statusScheduled {
			continue
		}
		result := reads[i].result
		if result.err != nil {
			// Terminal state so readers already waiting on this entry are not stranded. The engine
			// is bricked below, so the entry is never consulted again — the error reaches the waiter
			// over valueChan, not from the entry.
			entry.status = statusFailed
		} else if result.value == nil {
			entry.status = statusDeleted
			entry.value = nil
			c.trackLocked(entry, uint64(len(reads[i].key))+c.overheadPerEntry)
		} else {
			entry.status = statusAvailable
			entry.value = result.value
			c.trackLocked(entry, uint64(len(reads[i].key))+uint64(len(result.value))+c.overheadPerEntry)
		}
	}
	if failure != nil {
		c.takeOutOfServiceLocked(failure)
	}
	c.evictLocked(c.hardCapLocked())
	c.lock.Unlock()

	// The waiters for this batch were already released by resolveBatch, so there is nobody blocked
	// on us while reportReadFailure acquires the engine's versionLock.
	if failure != nil {
		c.reportReadFailure(failure)
	}
}

// Get a cache entry for a given key.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) entryLocked(key []byte, createIfMissing bool) *cacheEntry {
	// Indexing the map with string(key) does not copy the key; only the insert below does, which is
	// the one case that has to retain it.
	if entry, ok := c.entries[string(key)]; ok {
		return entry
	}
	if !createIfMissing {
		return nil
	}
	entry := newCacheEntry(c)
	c.entries[string(key)] = entry
	return entry
}

// entryLockedString is entryLocked for a caller that already holds the key as a string, which is
// then retained rather than copied.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) entryLockedString(key string, createIfMissing bool) *cacheEntry {
	if entry, ok := c.entries[key]; ok {
		return entry
	}
	if !createIfMissing {
		return nil
	}
	entry := newCacheEntry(c)
	c.entries[key] = entry
	return entry
}

// newCacheEntry returns an entry for a key whose state is not yet known.
func newCacheEntry(c *readCache) *cacheEntry {
	return &cacheEntry{
		cache:  c,
		status: statusUnknown,
	}
}

// putRetiredLocked installs data retired out of the shard's MVCC layer. A nil value marks the
// key as known-deleted (the engine-wide tombstone convention); any other value is cached as
// available. Inserts everything, then evicts overflow once at the end.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) putRetiredLocked(data map[string][]byte) {
	for k, v := range data {
		if v == nil {
			c.deleteRetiredLocked(k)
		} else {
			c.setRetiredLocked(k, v)
		}
	}

	// These insertions may have caused the cache to exceed its size budget, do necessary
	// evictions. setRetiredLocked does not evict on its own, so this is the enforcement point
	// for the bulk insert above.
	c.evictLocked(c.hardCapLocked())
}

// Set a retired value.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) setRetiredLocked(key string, value []byte) {
	entry := c.entryLockedString(key, true)
	entry.status = statusAvailable
	entry.value = value

	c.trackLocked(entry, uint64(len(key))+uint64(len(value))+c.overheadPerEntry)
}

// Delete a retired value.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) deleteRetiredLocked(key string) {
	entry := c.entryLockedString(key, false)
	if entry == nil {
		// Key is not in the cache, so nothing to do.
		return
	}
	entry.status = statusDeleted
	entry.value = nil

	c.trackLocked(entry, uint64(len(key))+c.overheadPerEntry)
}

// stampLocked records that a reader served a value from this entry in the current epoch.
//
// The load guards the store because the store is a locked instruction on amd64 while the load is a
// plain one, and an entry read more than once in an epoch only has to be stamped the first time. In a
// working set that fits, that makes a repeat read a plain load of a cache line the reader already
// pulled in to reach the value.
//
// The Locked postfix indicates that the caller must hold the shared lock. The stamp itself is atomic
// so that a shared lock suffices.
func (c *readCache) stampLocked(entry *cacheEntry) {
	if entry.lastRead.Load() != c.epoch {
		entry.lastRead.Store(c.epoch)
	}
}

// trackLocked records an entry's contribution to the size budget, replacing whatever it contributed
// before. Called whenever an entry takes on a value, including when it replaces one, so that a value
// changing size does not drift the total.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) trackLocked(entry *cacheEntry, size uint64) {
	if entry.size == 0 {
		c.trackedCount++
	}
	c.trackedBytes -= entry.size
	c.trackedBytes += size
	entry.size = size

	// A newly tracked entry counts as read now. Without this an entry inserted just before a sweep
	// looks infinitely old and is evicted immediately, which would throw away the read that fetched it.
	entry.lastRead.Store(c.epoch)
}

// untrackLocked removes an entry from the size budget and from the cache.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) untrackLocked(key string, entry *cacheEntry) {
	c.trackedBytes -= entry.size
	c.trackedCount--
	entry.size = 0
	delete(c.entries, key)
}

// evictSampleSize is the number of entries considered per eviction. Recency here is approximate by
// design: the cost of evicting a slightly-wrong entry is one read of the backing DB, never a wrong
// value, so sampling a few candidates buys most of the benefit of a true ordering for none of the
// bookkeeping. Redis, which evicts the same way, defaults to five.
const evictSampleSize = 8

// evictLocked evicts entries until the cache is within the given budget, choosing each victim as the
// oldest of a small sample.
//
// Sampling rather than an ordered structure is what keeps a read from having to write: maintaining a
// true LRU means reordering on every hit, which is why this cache needed an exclusive lock. The cost
// here is a function of the sample size and the number of evictions, and is independent of how large
// the cache has grown.
//
// Only entries holding a value are eligible. A scheduled entry has readers waiting on it and no value
// to reclaim, and a failed one belongs to a bricked engine. Such entries still count against the walk
// below, so that a cache holding many of them cannot turn each eviction into a scan of the whole map
// while the shard lock is held.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) evictLocked(budget uint64) {
	for c.trackedBytes > budget {
		var victimKey string
		var victim *cacheEntry
		oldest := uint64(math.MaxUint64)

		visited := 0
		for key, entry := range c.entries {
			visited++
			// An entry holding no value has nothing to reclaim and no stamp worth comparing, but it has
			// still consumed a step of the walk.
			if entry.size > 0 {
				if stamp := entry.lastRead.Load(); stamp <= oldest {
					oldest, victimKey, victim = stamp, key, entry
				}
			}
			if visited == evictSampleSize {
				break
			}
		}

		if victim == nil {
			// The walk found nothing to evict, either because the sample happened to hold no values or
			// because the accounting disagrees with the map. Stopping is the safe response either way:
			// running over budget costs memory, whereas looping here would spin while holding the shard
			// lock. The next maintenance pass samples a different part of the map and makes progress.
			return
		}
		c.untrackLocked(victimKey, victim)
	}
}

// sizeInfoLocked returns the current size (bytes) and entry count.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) sizeInfoLocked() (bytes uint64, entries uint64) {
	return c.trackedBytes, c.trackedCount
}

// evictionSlackDivisor sets how far over its budget the cache may run between maintenance passes, as
// a fraction of maxSize. A block's insertions are expected to fit inside the slack, so eviction
// normally happens once per block rather than on the path of whatever read happened to miss.
const evictionSlackDivisor = 16

// hardCapLocked is the ceiling that insertions enforce inline. Reaching it means a single block
// inserted more than the slack allows, so eviction happens on the insertion path as a backstop rather
// than letting the cache grow without bound until the next maintenance pass.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) hardCapLocked() uint64 {
	return c.maxSize + c.maxSize/evictionSlackDivisor
}

// maintainLocked advances the epoch and brings the cache back within its size budget.
//
// Called once per block rather than on every insertion, so the eviction work of a whole block is done
// in one pass while a lock is being taken anyway. Between passes the cache is allowed to run over its
// budget; the overshoot is bounded by the number of distinct keys a block can miss on, which is
// bounded by the reads in a block.
//
// The Locked postfix indicates that the caller must hold the shared lock.
func (c *readCache) maintainLocked() {
	c.epoch++
	c.evictLocked(c.maxSize)
}
