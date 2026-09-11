package view

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

// readCache is a read-through cache over the backing DB. Eviction order is approximate, and the cache
// is guarded by its shard's lock.
//
// Capitalized methods are the surface the shard calls; readCache is unexported, so they are not exports.
//
// Method postfixes state the lock contract: RLocked and WLocked require the caller to hold the read or
// write lock, Unlocked requires the caller to hold neither, and a bare name has no lock dependency.
type readCache struct {
	// Cancelled when the manager shuts down; interrupts blocked waits on in-flight reads.
	ctx context.Context

	// The manager's configuration. The cache reads only the fields that concern it.
	config *ViewManagerConfig

	// The underlying key-value database.
	db types.KeyValueDB

	// A pool for asynchronous reads.
	readPool threading.Pool

	// The shard's lock, borrowed by this cache.
	lock *sync.RWMutex

	// Maps the context cancellation observed by a blocked read to the manager's shutdown error:
	// the latched fatal error, or ErrViewManagerClosed on a clean close. Blocked reads select on ctx,
	// which is cancelled only when the manager shuts down, and the Close contract requires the
	// error released to such callers to wrap ErrViewManagerClosed or the fatal error.
	shutdownError func() error

	// Reports a failed DB read to the manager, which bricks and stops serving reads. Called
	// without the shard lock held, since it acquires the manager's versionLock.
	reportReadFailure func(error)

	// The failure that took this cache out of service, or nil while it is healthy. Set when the
	// manager bricks — for any reason, not only a failed read of this shard — after which the shard
	// refuses reads rather than serving data the manager can no longer vouch for.
	outOfServiceErr error

	// ViewManager-level metrics. Nil-safe; if nil, no metrics are recorded.
	metrics *ViewManagerMetrics

	// The maximum size of the cache, in bytes.
	maxSize uint64

	// The cached entries, keyed by string(key).
	entries map[string]*cacheEntry

	// The number of bytes counted toward the size budget. Only entries in a terminal data state
	// (available/deleted) are counted.
	trackedBytes uint64

	// The number of entries counted toward the size budget, where each entry counts for 1 regardless
	// of size.
	trackedCount uint64

	// Advanced once per maintenance pass, and stamped onto an entry whenever a value is served from it
	// or installed in it. Eviction prefers entries whose stamp is oldest.
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
	// being papered over. A failed read bricks the manager and takes the cache out of service (see
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

	// The channel carrying the result of the read currently in flight for this key. Non-nil
	// exactly while the status is statusScheduled: set by LookupWLocked when it schedules a read,
	// cleared by setTerminalEntryStateWLocked. Code running without the lock must use the channel
	// reference bound at scheduling time rather than reading this field.
	valueChan chan readResult

	// The epoch in which a reader last served a value from this entry.
	lastRead atomic.Uint64

	// This entry's contribution to trackedBytes, or zero while it holds no value.
	size uint64
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
// terminal result, or a wait plan that Resolve completes outside the lock. Classification never
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

// NewReadCache creates a readCache sharing the given lock (see the type doc for the locking
// contract).
func NewReadCache(
	ctx context.Context,
	config *ViewManagerConfig,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for asynchronous reads.
	readPool threading.Pool,
	// The shard's lock, borrowed by this cache.
	lock *sync.RWMutex,
	// The maximum size of the cache, in bytes.
	maxSize uint64,
	// Maps the context cancellation observed by a blocked read to the manager's shutdown error.
	shutdownError func() error,
	// Reports a failed DB read to the manager, which bricks and stops serving reads.
	reportReadFailure func(error),
) *readCache {
	return &readCache{
		ctx:               ctx,
		config:            config,
		db:                db,
		readPool:          readPool,
		lock:              lock,
		shutdownError:     shutdownError,
		reportReadFailure: reportReadFailure,
		maxSize:           maxSize,
		entries:           make(map[string]*cacheEntry),
	}
}

// readFromDB reads a single key from the underlying database, returning (nil, false, nil) if the
// key is not found and reserving errors for actual failures (e.g. I/O errors).
//
// A nil value with found == true is impossible: per the types.KeyValueDB.Get contract, a found
// zero-length value is a non-nil empty slice. The read-completion paths (injectValue, Resolve)
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

// setTerminalEntryStateWLocked records the entry's final status and value for this key and counts it toward
// the size budget, unless the status is statusFailed. Eviction is left to the caller.
func (e *cacheEntry) setTerminalEntryStateWLocked(key []byte, status valueStatus, value []byte) {
	e.status = status
	e.value = value
	// Every waiter holds the channel reference it was scheduled with (see injectValue), so
	// detaching here cannot strand one. It stops the entry from retaining the channel, and the
	// result buffered in it, for the rest of its life in the cache.
	e.valueChan = nil

	if status == statusFailed {
		return
	}
	e.cache.trackWLocked(e, uint64(len(key))+uint64(len(value))+e.cache.config.EstimatedOverheadPerEntry)
}

// AttemptFastLookupRLocked answers a read of the given key when the cache already holds the answer, which
// is either a value or the knowledge that the key is absent. A found value is never nil, so found
// distinguishes the two.
//
// ok is false when the cache holds no answer yet, including when a read of the key is already in
// flight; the caller must then retry under the write lock via LookupWLocked.
func (c *readCache) AttemptFastLookupRLocked(
	// The key to look up.
	key []byte,
	// If true, a cache hit marks the entry recently used. False is useful when an operation is
	// performed multiple times in close succession on the same key, since the stamp has non-zero
	// overhead and little benefit in that case.
	updateLru bool,
) (value []byte, found bool, ok bool) {
	entry := c.entryRLocked(key)
	if entry == nil {
		return nil, false, false
	}

	switch entry.status {
	case statusAvailable:
		if updateLru {
			entry.markRecentlyUsed(c.epoch)
		}
		return entry.value, true, true
	case statusDeleted:
		if updateLru {
			entry.markRecentlyUsed(c.epoch)
		}
		return nil, false, true
	default:
		return nil, false, false
	}
}

// LookupWLocked classifies a read of the given key and returns how to complete it: either an
// immediate terminal result, or a wait plan for Resolve. Pure state transition: it never blocks,
// and it performs the unknown -> scheduled transition under the lock, so a given read is
// scheduled by exactly one caller.
func (c *readCache) LookupWLocked(
	// The key to classify.
	key []byte,
	// If true, a cache hit marks the entry recently used. False is useful when an operation is
	// performed multiple times in close succession on the same key, since the stamp has non-zero
	// overhead and little benefit in that case.
	updateLru bool,
) lookupOutcome {
	entry := c.entryOrCreateWLocked(key)

	switch entry.status {
	case statusAvailable:
		if updateLru {
			entry.markRecentlyUsed(c.epoch)
		}
		return lookupOutcome{immediate: true, value: entry.value, found: true}
	case statusDeleted:
		if updateLru {
			entry.markRecentlyUsed(c.epoch)
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

// ResolveUnlocked completes a read classified by LookupWLocked. It submits the DB read when this
// caller owns scheduling, and may block until the in-flight read completes.
func (c *readCache) ResolveUnlocked(key []byte, outcome lookupOutcome) ([]byte, bool, error) {
	if outcome.immediate {
		c.metrics.reportCacheHits(1)
		return outcome.value, outcome.found, nil
	}

	c.metrics.reportCacheMisses(1)
	startTime := time.Now()

	if outcome.needsSchedule {
		entry := outcome.entry
		ch := outcome.valueChan
		c.readPool.Submit(func() {
			value, _, readErr := c.readFromDB(key)
			entry.injectValueUnlocked(key, ch, readResult{value: value, err: readErr})
		})
	}

	result, err := threading.InterruptiblePull(c.ctx, outcome.valueChan)
	c.metrics.reportCacheMissLatency(time.Since(startTime))
	if err != nil {
		// The pull is interrupted only by ctx cancellation, which means the manager is shutting
		// down; report the manager's shutdown error per the Close contract.
		return nil, false, fmt.Errorf("view manager shut down while awaiting read: %w", c.shutdownError())
	}
	outcome.valueChan <- result // reload the channel in case there are other listeners
	if result.err != nil {
		return nil, false, fmt.Errorf("scheduled read failed: %w", result.err)
	}
	return result.value, result.value != nil, nil
}

// ResolveBatchUnlocked completes the pending reads of a batch classified via LookupWLocked, writing
// found values into results. It schedules the not-yet-scheduled reads and blocks until every pending
// read completes, then applies the terminal cache states asynchronously (bulkInjectValuesUnlocked).
//
// A non-nil return means the whole batch failed. The first read error is returned after the full
// drain, unless the manager shuts down first.
func (c *readCache) ResolveBatchUnlocked(pending []pendingRead, results map[string][]byte) error {
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
				p.valueChan <- readResult{value: value, err: readErr}
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
			// pool is being torn down, so bail immediately with the manager's shutdown error
			// per the Close contract.
			return fmt.Errorf("view manager shut down while awaiting batch read: %w", c.shutdownError())
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
	go c.bulkInjectValuesUnlocked(pending)

	return firstErr
}

// This method is called by the read scheduler when a value becomes available. ch is the channel
// bound at scheduling time (see Resolve), which is the one every waiter on this read is blocked
// on; e.valueChan may already have been detached by then.
func (e *cacheEntry) injectValueUnlocked(key []byte, ch chan readResult, result readResult) {
	c := e.cache
	c.lock.Lock()

	// The failure to report to the manager. The read error wins when both happen, since it is the one
	// the waiter is about to be handed.
	failure := result.err

	if e.status == statusScheduled {
		if result.err != nil {
			// Terminal state so readers already waiting on this entry are not stranded. The manager
			// is bricked below, so the entry is never consulted again — the error reaches the waiter
			// over the bound channel, not from the entry.
			e.setTerminalEntryStateWLocked(key, statusFailed, nil)
		} else if result.value == nil {
			e.setTerminalEntryStateWLocked(key, statusDeleted, nil)
			failure = c.evictWLocked(c.hardCap())
		} else {
			e.setTerminalEntryStateWLocked(key, statusAvailable, result.value)
			failure = c.evictWLocked(c.hardCap())
		}
	}

	// Take the cache out of service regardless of the entry's status: the DB read failed, which is
	// fatal whether or not this entry was still the one waiting on it.
	if result.err != nil {
		c.TakeOutOfServiceWLocked(result.err)
	}

	c.lock.Unlock()

	// Release the waiter before bricking. reportReadFailure acquires the manager's versionLock, and
	// nobody may be blocked on us while we wait for it.
	ch <- result

	if failure != nil {
		c.reportReadFailure(failure)
	}
}

// Applies deferred cache updates for a batch of reads under a single lock acquisition.
func (c *readCache) bulkInjectValuesUnlocked(reads []pendingRead) {
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
		key := []byte(reads[i].key)
		result := reads[i].result
		if result.err != nil {
			// Terminal state so readers already waiting on this entry are not stranded. The manager
			// is bricked below, so the entry is never consulted again — the error reaches the waiter
			// over the bound channel, not from the entry.
			entry.setTerminalEntryStateWLocked(key, statusFailed, nil)
		} else if result.value == nil {
			entry.setTerminalEntryStateWLocked(key, statusDeleted, nil)
		} else {
			entry.setTerminalEntryStateWLocked(key, statusAvailable, result.value)
		}
	}
	if failure != nil {
		c.TakeOutOfServiceWLocked(failure)
	}
	if err := c.evictWLocked(c.hardCap()); err != nil && failure == nil {
		failure = err
	}
	c.lock.Unlock()

	// The waiters for this batch were already released by ResolveBatch, so there is nobody blocked
	// on us while reportReadFailure acquires the manager's versionLock.
	if failure != nil {
		c.reportReadFailure(failure)
	}
}

// entryRLocked returns the cache entry for a given key, or nil if the cache holds none.
func (c *readCache) entryRLocked(key []byte) *cacheEntry {
	return c.entries[string(key)]
}

// entryOrCreateWLocked returns the cache entry for a given key, creating one whose value is not yet
// known if the cache holds none. Never returns nil.
func (c *readCache) entryOrCreateWLocked(key []byte) *cacheEntry {
	if entry, ok := c.entries[string(key)]; ok {
		return entry
	}
	entry := &cacheEntry{
		cache:  c,
		status: statusUnknown,
	}
	c.entries[string(key)] = entry
	return entry
}

// PutRetiredWLocked installs data retired out of the shard's MVCC layer. A nil value marks the
// key as known-deleted (the manager-wide tombstone convention); any other value is cached as
// available. Inserts everything, then evicts overflow once at the end.
func (c *readCache) PutRetiredWLocked(data map[string][]byte) error {
	for k, v := range data {
		if v == nil {
			c.deleteRetiredWLocked([]byte(k))
		} else {
			c.setRetiredWLocked([]byte(k), v)
		}
	}

	// These insertions may have caused the cache to exceed its size budget, do necessary
	// evictions. setRetiredWLocked does not evict on its own, so this is the enforcement point
	// for the bulk insert above.
	return c.evictWLocked(c.hardCap())
}

// Set a retired value.
func (c *readCache) setRetiredWLocked(key []byte, value []byte) {
	entry := c.entryOrCreateWLocked(key)
	entry.setTerminalEntryStateWLocked(key, statusAvailable, value)
}

// Delete a retired value.
func (c *readCache) deleteRetiredWLocked(key []byte) {
	entry := c.entryRLocked(key)
	if entry == nil {
		// Key is not in the cache, so nothing to do.
		return
	}
	entry.setTerminalEntryStateWLocked(key, statusDeleted, nil)
}

// markRecentlyUsed records that a reader served a value from this entry in the given epoch, making it
// a later candidate for eviction.
func (e *cacheEntry) markRecentlyUsed(epoch uint64) {
	if e.lastRead.Load() != epoch {
		// The load guards the store to keep this entry's cache line in shared state on a repeat read:
		// concurrent readers need that same line for the value, and storing would take it exclusive.
		e.lastRead.Store(epoch)
	}
}

// trackWLocked records an entry's contribution to the size budget, replacing whatever it contributed
// before, and stamps the entry as read in the current epoch.
func (c *readCache) trackWLocked(entry *cacheEntry, size uint64) {
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

// untrackWLocked removes an entry from the size budget and from the cache.
func (c *readCache) untrackWLocked(key string, entry *cacheEntry) {
	c.trackedBytes -= entry.size
	c.trackedCount--
	entry.size = 0
	delete(c.entries, key)
}

// evictWLocked evicts entries until the cache is within the given budget, choosing each victim as the
// oldest of a small sample of candidates.
func (c *readCache) evictWLocked(budget uint64) error {
	for c.trackedBytes > budget {
		var victimKey string
		var victim *cacheEntry
		oldest := uint64(math.MaxUint64)

		var visited uint64
		for key, entry := range c.entries {
			if entry.status == statusScheduled {
				// A read still in flight must not be evicted: untracking leaves its status untouched,
				// so the completing read would re-track an entry no longer in the map.
				continue
			}

			stamp := entry.lastRead.Load()
			if stamp <= oldest {
				oldest, victimKey, victim = stamp, key, entry
			}

			visited++
			if visited == c.config.EvictionSampleSize {
				break
			}
		}

		if victim == nil {
			// Unreachable while the accounting agrees with the map.
			err := fmt.Errorf("read cache accounting is corrupt: %d tracked bytes exceed budget %d, "+
				"but none of the %d entries is an eviction candidate",
				c.trackedBytes, budget, len(c.entries))
			c.TakeOutOfServiceWLocked(err)
			return err
		}
		c.untrackWLocked(victimKey, victim)
	}
	return nil
}

// SizeInfoRLocked returns the current size (bytes) and entry count.
func (c *readCache) SizeInfoRLocked() (bytes uint64, entries uint64) {
	return c.trackedBytes, c.trackedCount
}

// hardCap is the ceiling that insertions enforce inline.
func (c *readCache) hardCap() uint64 {
	return c.maxSize + c.maxSize/c.config.EvictionSlackDivisor
}

// MaintainWLocked advances the epoch and brings the cache back within its size budget.
func (c *readCache) MaintainWLocked() error {
	c.epoch++
	return c.evictWLocked(c.maxSize)
}

// ErrIfOutOfServiceRLocked returns a second-hand error if this cache has been taken out of service, or nil
// while it is healthy. The error is inherited from the earlier failure rather than produced by the
// caller's own operation.
func (c *readCache) ErrIfOutOfServiceRLocked() error {
	if c.outOfServiceErr == nil {
		return nil
	}
	return fmt.Errorf("shard is out of service: %w", c.outOfServiceErr)
}

// TakeOutOfServiceWLocked records the failure that stops this cache from serving reads. The first
// failure wins; later ones are dropped so the reported cause is the original one.
func (c *readCache) TakeOutOfServiceWLocked(err error) {
	if c.outOfServiceErr == nil {
		c.outOfServiceErr = err
	}
}
