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
// To amortize the keymap's per-write fsync, the manager coalesces scheduled work into a single Put and a
// single Delete per cycle, up to maxBatchSize keys, flushing a partial batch once maxFlushInterval elapses.
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

	// the maximum number of keys deleted from the keymap in a single keymap.Delete call
	deleteBatchSize uint64

	// the maximum time a batch is allowed to accumulate before it is applied, even if not full
	maxFlushInterval time.Duration

	// deletionWatermarkChan publishes the highest segment index whose keymap entries have been durably deleted
	// to the control loop, which gates segment-file deletion on it. Sends are fire-and-forget (see
	// publishDeletionWatermark): the manager must never block on the control loop, or it could deadlock the
	// seal path (control loop -> flush loop -> manager -> control loop). A lagging watermark only delays file
	// deletion, never causing a premature one.
	deletionWatermarkChan chan int64
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
	deleteBatchSize uint64,
	maxFlushInterval time.Duration,
	deletionWatermarkChan chan int64,
) *keymapManager {

	return &keymapManager{
		errorMonitor:          errorMonitor,
		keymap:                keymap,
		unflushedDataCache:    unflushedDataCache,
		metrics:               metrics,
		clock:                 clock,
		name:                  name,
		requestChan:           make(chan any, channelSize),
		maxBatchSize:          maxBatchSize,
		deleteBatchSize:       deleteBatchSize,
		maxFlushInterval:      maxFlushInterval,
		deletionWatermarkChan: deletionWatermarkChan,
	}
}

// publishDeletionWatermark notifies the control loop that every segment up to and including watermark has had
// its keymap entries durably deleted. The send is fire-and-forget and never blocks the manager (blocking here
// could deadlock the seal path). If the channel is full the update is dropped: the watermark is monotonic and
// the control loop drains it each GC pass, so a later publish delivers an equal-or-newer value. A dropped
// update only delays file deletion, which is always safe.
func (m *keymapManager) publishDeletionWatermark(watermark int64) {
	select {
	case m.deletionWatermarkChan <- watermark:
	default:
	}
}

// scheduleWrite enqueues a batch of newly-durable keys to be put into the keymap. Blocks if the manager's
// channel is full (backpressure); returns an error only if the DB is panicking.
func (m *keymapManager) scheduleWrite(keys []*types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}
	return util.Send(m.errorMonitor, m.requestChan, &keymapWriteRequest{keys: keys})
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
func (m *keymapManager) run() {
	for {
		// Block until there is work to do (or a panic forces an immediate exit).
		var message any
		select {
		case <-m.errorMonitor.ImmediateShutdownRequired():
			return
		case message = <-m.requestChan:
		}

		// A barrier (shutdown or sync) with no batch in progress: all prior work is already applied (FIFO).
		switch barrier := message.(type) {
		case *keymapManagerShutdownRequest:
			barrier.shutdownCompleteChan <- struct{}{}
			return
		case *keymapManagerSyncRequest:
			barrier.doneChan <- struct{}{}
			continue
		}

		// Start put and delete batches from the first request, then coalesce more work into them.
		var puts []*types.ScopedKey
		var deletes []*types.ScopedKey
		maxDeletedSegment := int64(-1)
		switch req := message.(type) {
		case *keymapWriteRequest:
			puts = req.keys
		case *keymapDeleteRequest:
			deletes = req.keys
			maxDeletedSegment = req.segment
		}

		barrier, ok := m.accumulate(&puts, &deletes, &maxDeletedSegment)
		if !ok {
			// A panic forced an immediate shutdown; abandon the batch (repair restores puts on restart).
			return
		}

		// Apply puts before deletes so a put followed by a delete of the same key resolves to deleted.
		if err := m.writeBatch(puts); err != nil {
			m.errorMonitor.Panic(fmt.Errorf("failed to write keys to keymap: %w", err))
			return
		}
		if err := m.deleteBatch(deletes); err != nil {
			m.errorMonitor.Panic(fmt.Errorf("failed to delete keys from keymap: %w", err))
			return
		}

		// The deletes are now durable, so it is safe to let the control loop delete the corresponding segment
		// files. maxDeletedSegment is monotonically non-decreasing across batches (FIFO, oldest-first).
		if maxDeletedSegment >= 0 {
			m.publishDeletionWatermark(maxDeletedSegment)
		}

		// Handle the barrier (if any) that ended accumulation, now that the batch has been applied.
		switch b := barrier.(type) {
		case *keymapManagerShutdownRequest:
			b.shutdownCompleteChan <- struct{}{}
			return
		case *keymapManagerSyncRequest:
			b.doneChan <- struct{}{}
		}
	}
}

// accumulate grows the put and delete batches with further scheduled work until their combined size reaches
// maxBatchSize or maxFlushInterval elapses since the batch began, whichever comes first. maxDeletedSegment is
// updated to the highest segment index seen across the coalesced delete requests.
//
// Returns a non-nil barrier request (shutdown or sync) if one is received, so the caller can apply the batch
// before acting on it. The boolean is false if a panic-induced shutdown was signalled, in which case the
// manager must abort without applying.
func (m *keymapManager) accumulate(
	puts *[]*types.ScopedKey,
	deletes *[]*types.ScopedKey,
	maxDeletedSegment *int64,
) (any, bool) {

	timer := time.NewTimer(m.maxFlushInterval)
	defer timer.Stop()

	for len(*puts)+len(*deletes) < m.maxBatchSize {
		select {
		case <-m.errorMonitor.ImmediateShutdownRequired():
			return nil, false
		case <-timer.C:
			return nil, true
		case message := <-m.requestChan:
			switch req := message.(type) {
			case *keymapManagerShutdownRequest:
				return req, true
			case *keymapManagerSyncRequest:
				return req, true
			case *keymapWriteRequest:
				*puts = append(*puts, req.keys...)
			case *keymapDeleteRequest:
				*deletes = append(*deletes, req.keys...)
				if req.segment > *maxDeletedSegment {
					*maxDeletedSegment = req.segment
				}
			}
		}
	}
	return nil, true
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

// deleteBatch deletes a batch of garbage-collected keys from the keymap, in chunks of deleteBatchSize to bound
// the size of any single keymap.Delete. An empty batch is a no-op. The keys belong to segments being
// collected; they are long since flushed, so there is nothing to prune from the unflushed data cache.
func (m *keymapManager) deleteBatch(keys []*types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}

	for start := uint64(0); start < uint64(len(keys)); start += m.deleteBatchSize {
		end := start + m.deleteBatchSize
		if end > uint64(len(keys)) {
			end = uint64(len(keys))
		}
		if err := m.keymap.Delete(keys[start:end]); err != nil {
			return fmt.Errorf("failed to delete keys from keymap: %w", err)
		}
	}

	return nil
}
