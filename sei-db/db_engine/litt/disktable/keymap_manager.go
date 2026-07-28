package disktable

import (
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// keymapWriteRequest carries a batch of newly-durable keys to be written (put) into the keymap.
type keymapWriteRequest struct {
	keys []*types.ScopedKey

	// uncompressedPutBytes is the total raw (pre-compression) value bytes represented by the primary keys in
	// this batch. It feeds pendingPutBytes to bound the raw memory footprint of the unflushed-data cache,
	// independent of how the values are represented on disk.
	uncompressedPutBytes uint64
}

// keymapDeleteRequest carries the keys of a garbage-collected segment to be deleted from the keymap. segment
// is the index of the segment being collected; once this request is applied, the manager advances its
// deletion watermark to that index, which is what permits the control loop to delete the segment's files.
type keymapDeleteRequest struct {
	keys    []*types.ScopedKey
	segment int64
}

// keymapManagerShutdownRequest asks the manager to apply all queued work and stop. Because the manager
// consumes its channel in FIFO order, by the time it processes this request every previously scheduled put
// and delete has already been applied.
type keymapManagerShutdownRequest struct {
	shutdownCompleteChan chan struct{}
}

// keymapManagerSyncRequest asks the manager to apply all queued work (advancing the deletion watermark) and
// then signal, without stopping. FIFO ordering means every previously scheduled put and delete has been
// applied by the time it processes this request. Used to make an explicit RunGC() deterministic.
type keymapManagerSyncRequest struct {
	doneChan chan struct{}
}

// pendingDelete is a garbage-collected segment's keys awaiting deletion from the keymap. The manager drains
// it incrementally (deleteBatchSize keys per sub-batch) so a large delete never blocks latency-critical
// puts. offset is the number of keys already deleted from the front of keys.
type pendingDelete struct {
	segment int64
	keys    []*types.ScopedKey
	offset  int
}

// keymapManager asynchronously applies keymap mutations on a single goroutine, off the control loop: puts
// (newly-durable keys from flushes and seals) and deletes (keys of garbage-collected segments). Keeping all
// keymap mutation on one FIFO consumer means writes stay in key-file append order (repairKeymap relies on the
// keymap being a prefix of that order) and a put for a key is always applied before a later delete of it.
//
// Keys are crash-durable in their segment before a put is scheduled here, so the keymap may lag freely on the
// put side (a crash is repaired from the segment key files; see repairKeymap). Deletes are different: once a
// segment's files are gone, a leftover keymap entry cannot be repaired, so the manager only advances the
// deletion watermark after its keymap.Delete returns (a durable, synced delete for the production keymap), and
// the control loop deletes a segment's files only after the watermark covers it.
//
// Puts are latency-critical (they gate write throughput); deletes are background cleanup (they only gate
// reclamation of segment files, which may lag freely). The manager therefore prioritizes puts: every cycle it
// applies the accumulated put batch before applying a single delete sub-batch, and it drains a large delete
// backlog incrementally so a garbage-collection burst cannot stall writes. To amortize the keymap's per-write
// fsync, puts coalesce into a single Put of up to maxBatchSize keys, flushing a partial batch once
// maxFlushInterval elapses. A runaway delete backlog is bounded by maxBufferedDeletes: at that high-water mark
// the manager stops accepting new work (so the channel backpressures producers) until the backlog drains.
type keymapManager struct {
	// handles fatal DB errors and signals an immediate (panic) shutdown
	errorMonitor *util.ErrorMonitor

	// the keymap that keys are put into and deleted from
	keymap keymap.Keymap

	// unflushedDataCache holds keys that have been written but not yet persisted to the keymap. The manager
	// prunes a key from it once that key is durable in the keymap. Shared with the disk table (reads happen on
	// caller threads); access is via sync.Map, which is goroutine safe. This is the one piece of shared state
	// the manager touches directly instead of via a channel.
	unflushedDataCache *sync.Map

	// metrics encapsulates DB metrics (may be nil).
	metrics *metrics.LittDBMetrics

	// clock provides the current time, used for metrics.
	clock func() time.Time

	// the name of the table, used for metric labels
	name string

	// bounded channel of work; a full channel backpressures the flush loop (and therefore writes)
	requestChan chan any

	// the maximum number of keys coalesced into a single keymap Put or Delete
	maxBatchSize int

	// the maximum number of raw (pre-compression) value bytes a coalescing put batch may represent before it is
	// applied, independent of key count. Bounds the raw memory footprint of the unflushed-data cache when values
	// are large (few keys, many bytes). Measured in raw bytes because that is what the cache holds, independent
	// of the compressed on-disk size.
	maxBatchBytes uint64

	// the maximum number of keys deleted from the keymap in a single keymap.Delete call
	deleteBatchSize uint64

	// the maximum time a batch is allowed to accumulate before it is applied, even if not full
	maxFlushInterval time.Duration

	// maxBufferedDeletes is the high-water mark for bufferedDeleteCount; the manager applies backpressure
	// (stops popping the channel) at this many buffered delete keys and resumes once it falls to half.
	maxBufferedDeletes uint64

	// The following fields are mutated only by the run goroutine and require no synchronization.

	// puts is the put batch currently being accumulated. It is applied (one keymap.Put) before each delete
	// sub-batch, so a put and a same-key delete in flight resolve to deleted (the delete wins).
	puts []*types.ScopedKey

	// pendingPutBytes is the total raw (pre-compression) value bytes represented by the primary keys in puts.
	// Secondary keys are zero-copy sub-ranges of their primary's value in the unflushed-data cache, so counting
	// primaries alone tracks the raw cache memory this batch will free when applied. Raw (not on-disk) bytes,
	// since that is the memory the cache actually holds. Reset to zero whenever puts is applied.
	pendingPutBytes uint64

	// deleteBacklog holds garbage-collected segments' keys awaiting deletion, in arrival (FIFO) order —
	// oldest segment first. It is drained one sub-batch at a time, interleaved with puts, so a large delete
	// burst cannot block writes.
	deleteBacklog []*pendingDelete

	// bufferedDeleteCount is the total number of un-applied keys across deleteBacklog.
	bufferedDeleteCount uint64

	// backpressure is true while the manager is shedding new work (not popping the channel) to drain a
	// delete backlog that reached maxBufferedDeletes; it clears once the backlog falls to half.
	backpressure bool

	// flushTimer bounds how long a partial put batch waits before being applied. It is armed when a batch's
	// first key is buffered and stopped when the batch is applied; nil when no put batch is pending.
	flushTimer *time.Timer

	// controlLoop is the reader and owner of the deletion-watermark channel. After the manager durably deletes a
	// segment's keymap entries it reports the highest such segment index by calling
	// controlLoop.publishDeletionWatermark, which gates segment-file reclamation. That call is fire-and-forget:
	// the manager must never block on the control loop, or it could deadlock the seal path
	// (control loop -> flush loop -> manager -> control loop). A lagging watermark only delays file deletion,
	// never causing a premature one.
	controlLoop *controlLoop
}

// newKeymapManager creates a keymap manager. Call run() in a dedicated goroutine to start it.
func newKeymapManager(
	errorMonitor *util.ErrorMonitor,
	keymap keymap.Keymap,
	unflushedDataCache *sync.Map,
	metrics *metrics.LittDBMetrics,
	clock func() time.Time,
	name string,
	channelSize int,
	maxBatchSize int,
	maxBatchBytes uint64,
	deleteBatchSize uint64,
	maxFlushInterval time.Duration,
	maxBufferedDeletes uint64,
) *keymapManager {

	return &keymapManager{
		errorMonitor:       errorMonitor,
		keymap:             keymap,
		unflushedDataCache: unflushedDataCache,
		metrics:            metrics,
		clock:              clock,
		name:               name,
		requestChan:        make(chan any, channelSize),
		maxBatchSize:       maxBatchSize,
		maxBatchBytes:      maxBatchBytes,
		deleteBatchSize:    deleteBatchSize,
		maxFlushInterval:   maxFlushInterval,
		maxBufferedDeletes: maxBufferedDeletes,
	}
}

// scheduleWrite enqueues a batch of newly-durable keys to be put into the keymap. uncompressedPutBytes is the
// total raw (pre-compression) value bytes the batch's primary keys represent, used to bound the unflushed-data
// cache. Blocks if the manager's channel is full (backpressure); returns an error only if the DB is panicking.
func (m *keymapManager) scheduleWrite(keys []*types.ScopedKey, uncompressedPutBytes uint64) error {
	if len(keys) == 0 {
		return nil
	}
	return util.Send(m.errorMonitor, m.requestChan,
		&keymapWriteRequest{keys: keys, uncompressedPutBytes: uncompressedPutBytes})
}

// scheduleDelete enqueues a garbage-collected segment's keys to be deleted from the keymap. segment is the
// index of the segment being collected; the manager advances its deletion watermark to it once applied.
// Blocks if the manager's channel is full; returns an error only if the DB is panicking.
func (m *keymapManager) scheduleDelete(keys []*types.ScopedKey, segment int64) error {
	if len(keys) == 0 {
		return nil
	}
	return util.Send(m.errorMonitor, m.requestChan, &keymapDeleteRequest{keys: keys, segment: segment})
}

// drain asks the manager to apply all queued work and stop, blocking until it has done so. Used on clean
// shutdown, after the flush loop and control loop have stopped scheduling new work.
func (m *keymapManager) drain() error {
	shutdownCompleteChan := make(chan struct{}, 1)
	request := &keymapManagerShutdownRequest{shutdownCompleteChan: shutdownCompleteChan}
	if err := util.Send(m.errorMonitor, m.requestChan, request); err != nil {
		return fmt.Errorf("failed to send keymap manager shutdown request: %w", err)
	}
	if _, err := util.Await(m.errorMonitor, shutdownCompleteChan); err != nil {
		return fmt.Errorf("failed to await keymap manager drain: %w", err)
	}
	return nil
}

// sync blocks until every put and delete scheduled before this call has been applied to the keymap and the
// deletion watermark advanced accordingly. The manager keeps running afterwards.
func (m *keymapManager) sync() error {
	doneChan := make(chan struct{}, 1)
	request := &keymapManagerSyncRequest{doneChan: doneChan}
	if err := util.Send(m.errorMonitor, m.requestChan, request); err != nil {
		return fmt.Errorf("failed to send keymap manager sync request: %w", err)
	}
	if _, err := util.Await(m.errorMonitor, doneChan); err != nil {
		return fmt.Errorf("failed to await keymap manager sync: %w", err)
	}
	return nil
}

// run is the manager's event loop. It exits when it receives a shutdown request (clean shutdown, after
// draining all queued work) or when the error monitor signals an immediate shutdown (panic).
//
// Each cycle coalesces immediately-available work, then either applies a batch or blocks for more. A batch is
// applied once the put batch is full (by key count or by the value bytes it represents) or a delete backlog
// exists: puts are applied first (so a put followed by a delete of the same key resolves to deleted), then a
// single delete sub-batch, which keeps puts flowing while still making steady progress on a delete backlog.
func (m *keymapManager) run() {
	for {
		if m.coalesce() {
			return
		}

		if len(m.puts) >= m.maxBatchSize || m.pendingPutBytes >= m.maxBatchBytes || len(m.deleteBacklog) > 0 {
			// Enough work to apply: puts first (delete wins on a same-key collision), then one delete
			// sub-batch so the backlog drains without starving puts.
			if !m.flushPuts() {
				return
			}
			if len(m.deleteBacklog) > 0 && !m.applyDeleteSubBatch() {
				return
			}
			continue
		}

		// Only a partial put batch (or nothing) is pending: block for more work, bounding a partial batch's
		// wait with the flush timer.
		if m.awaitWork() {
			return
		}
	}
}

// refreshBackpressure engages backpressure at the buffered-delete high-water mark and releases it once the
// backlog falls to half (hysteresis to avoid thrashing). It is called wherever bufferedDeleteCount changes, so
// coalesce always sees an up-to-date flag — including mid-coalesce, which is what stops an unbounded delete
// stream from growing the backlog without limit. While engaged the manager stops popping the channel, so
// producers block until the backlog drains.
func (m *keymapManager) refreshBackpressure() {
	if m.bufferedDeleteCount >= m.maxBufferedDeletes {
		m.backpressure = true
	} else if m.bufferedDeleteCount <= m.maxBufferedDeletes/2 {
		m.backpressure = false
	}
}

// coalesce drains immediately-available requests into the put batch and delete backlog without blocking,
// stopping once the put batch is full (by key count or represented value bytes), the delete backlog reaches
// its high-water mark (backpressure engages), or no request is ready. Returns true if the manager must stop.
func (m *keymapManager) coalesce() bool {
	for len(m.puts) < m.maxBatchSize && m.pendingPutBytes < m.maxBatchBytes && !m.backpressure {
		select {
		case <-m.errorMonitor.ImmediateShutdownRequired():
			return true
		case msg := <-m.requestChan:
			if m.routeRequest(msg) {
				return true
			}
		default:
			return false
		}
	}
	return false
}

// awaitWork blocks until the next request arrives, or until a pending partial put batch's flush timer fires
// (whichever first), and applies the result. Returns true if the manager must stop.
func (m *keymapManager) awaitWork() bool {
	m.armFlushTimer()
	select {
	case <-m.errorMonitor.ImmediateShutdownRequired():
		return true
	case msg := <-m.requestChan:
		return m.routeRequest(msg)
	case <-m.flushTimerChan():
		return !m.flushPuts()
	}
}

// routeRequest dispatches one received request. Puts append to the put batch; deletes append to the backlog;
// sync/shutdown barriers drain all buffered work first (FIFO guarantee) and then signal. Returns true iff the
// manager must stop (shutdown, or a panic during a barrier drain).
func (m *keymapManager) routeRequest(msg any) bool {
	switch req := msg.(type) {
	case *keymapWriteRequest:
		m.puts = append(m.puts, req.keys...)
		m.pendingPutBytes += req.uncompressedPutBytes
	case *keymapDeleteRequest:
		m.enqueueDelete(req)
	case *keymapManagerSyncRequest:
		if !m.drainAll() {
			return true
		}
		req.doneChan <- struct{}{}
	case *keymapManagerShutdownRequest:
		if !m.drainAll() {
			// drainAll only fails after calling Panic(); leave the completion unsignaled so the awaiting
			// drain() observes the cancelled context and reports the failure, mirroring the sync barrier.
			return true
		}
		req.shutdownCompleteChan <- struct{}{}
		return true
	default:
		m.errorMonitor.Panic(fmt.Errorf("unknown keymap manager message type %T", msg))
		return true
	}
	return false
}

// drainAll applies every buffered put and the entire delete backlog. Sync/shutdown barriers use it to honor
// their contract that all previously-scheduled work is durable before they return. Returns false on a
// panic-induced failure.
func (m *keymapManager) drainAll() bool {
	if !m.flushPuts() {
		return false
	}
	for len(m.deleteBacklog) > 0 {
		if !m.applyDeleteSubBatch() {
			return false
		}
	}
	return true
}

// flushPuts applies the accumulated put batch (if any) with a single keymap.Put() and stops the flush timer.
// Returns false on a panic-induced failure, signalling that the manager must stop.
func (m *keymapManager) flushPuts() bool {
	if len(m.puts) > 0 {
		if err := m.writeBatch(m.puts); err != nil {
			m.errorMonitor.Panic(fmt.Errorf("failed to write keys to keymap: %w", err))
			return false
		}
		m.puts = nil
		m.pendingPutBytes = 0
	}
	m.stopFlushTimer()
	return true
}

// armFlushTimer starts the flush-interval timer for the current put batch if a batch is pending and the timer
// is not already running. Anchoring the timer to the batch's first key bounds any key's wait to
// maxFlushInterval. A no-op when no put batch is pending.
func (m *keymapManager) armFlushTimer() {
	if len(m.puts) > 0 && m.flushTimer == nil {
		m.flushTimer = time.NewTimer(m.maxFlushInterval)
	}
}

// stopFlushTimer stops and clears the flush-interval timer if running.
func (m *keymapManager) stopFlushTimer() {
	if m.flushTimer != nil {
		m.flushTimer.Stop()
		m.flushTimer = nil
	}
}

// flushTimerChan returns the flush-interval timer's channel, or nil (which blocks forever in a select) when
// no put batch is pending.
func (m *keymapManager) flushTimerChan() <-chan time.Time {
	if m.flushTimer == nil {
		return nil
	}
	return m.flushTimer.C
}

// enqueueDelete appends a garbage-collected segment's keys to the delete backlog.
func (m *keymapManager) enqueueDelete(req *keymapDeleteRequest) {
	if len(req.keys) == 0 {
		return
	}
	m.deleteBacklog = append(m.deleteBacklog, &pendingDelete{segment: req.segment, keys: req.keys})
	m.bufferedDeleteCount += uint64(len(req.keys))
	m.refreshBackpressure()
}

// applyDeleteSubBatch deletes up to deleteBatchSize keys from the front backlog group. When that group is
// fully drained it publishes the deletion watermark for its segment (which permits the control loop to delete
// the segment's files) and removes the group. Backlog groups are FIFO oldest-segment-first, so the published
// watermark is monotonically non-decreasing. Returns false on a panic-induced failure.
func (m *keymapManager) applyDeleteSubBatch() bool {
	front := m.deleteBacklog[0]
	// deleteBatchSize is a bounded config value (GCBatchSize, validated >= 1), so it fits an int.
	end := front.offset + int(m.deleteBatchSize) //nolint:gosec // bounded batch size, fits int
	if end > len(front.keys) {
		end = len(front.keys)
	}
	chunk := front.keys[front.offset:end]

	if err := m.keymap.Delete(chunk); err != nil {
		m.errorMonitor.Panic(fmt.Errorf("failed to delete keys from keymap: %w", err))
		return false
	}
	m.bufferedDeleteCount -= uint64(len(chunk))
	m.refreshBackpressure()
	front.offset = end

	if front.offset >= len(front.keys) {
		// The whole segment's keymap entries are durably deleted; it is now safe for the control loop to
		// delete the segment's files.
		m.controlLoop.publishDeletionWatermark(front.segment)
		m.deleteBacklog[0] = nil
		m.deleteBacklog = m.deleteBacklog[1:]
	}
	return true
}

// writeBatch puts a batch of keys into the keymap and then prunes them from the unflushed data cache. The
// prune happens only after the keymap Put succeeds, so a key is never absent from both the keymap and the
// unflushed cache. An empty batch is a no-op.
func (m *keymapManager) writeBatch(keys []*types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}

	if m.metrics != nil {
		start := m.clock()
		defer func() {
			m.metrics.ReportKeymapFlushLatency(m.name, m.clock().Sub(start))
		}()
	}

	if err := m.keymap.Put(keys); err != nil {
		return fmt.Errorf("failed to write keys to keymap: %w", err)
	}

	// The keys are now durable in both the segment and the keymap, so it is safe to drop them from the
	// unflushed data cache.
	for _, key := range keys {
		m.unflushedDataCache.Delete(util.UnsafeBytesToString(key.Key))
	}

	return nil
}
