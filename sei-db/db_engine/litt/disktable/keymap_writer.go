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

// keymapWriteRequest carries a batch of newly-durable keys to be written into the keymap.
type keymapWriteRequest struct {
	keys []*types.ScopedKey

	// sealedSegment is the index of the segment that this request completes: once this request is applied,
	// every key of that segment (and every earlier segment) is durable in the keymap. It is -1 for a mutable-
	// segment flush, which does not complete any segment. Used to advance the writer's flushed watermark.
	sealedSegment int64
}

// keymapWriterShutdownRequest asks the keymap writer to apply all queued writes and stop. Because the
// writer consumes its channel in FIFO order, by the time it processes this request every previously
// scheduled write has already been applied.
type keymapWriterShutdownRequest struct {
	shutdownCompleteChan chan struct{}
}

// keymapWriterSyncRequest asks the keymap writer to apply all queued writes (advancing the flushed
// watermark) and then signal, without stopping. Because the writer consumes its channel in FIFO order, by
// the time it processes this request every previously scheduled write has been applied. Used to make an
// explicit RunGC() deterministic: the keymap must be caught up before GC can collect a segment.
type keymapWriterSyncRequest struct {
	doneChan chan struct{}
}

// keymapWriter asynchronously writes keys into the keymap. Keys are made crash-durable in their segment
// before they are scheduled here (see the flush loop), so the keymap is permitted to lag behind the
// segments: a crash that loses queued writes is repaired from the segment key files on the next startup
// (see DiskTable.repairKeymap).
//
// The writer is the single consumer of its channel, which keeps keymap writes in key-file append order.
// That ordering is load-bearing: repairKeymap relies on the keymap always being a prefix of the key-file
// write order, and the flushed watermark (below) relies on keys arriving in segment order.
//
// To amortize the keymap's per-write fsync, the writer coalesces scheduled work into a single keymap.Put,
// up to maxBatchSize keys. It deliberately lets a partial batch build up rather than flushing the instant
// the channel empties, so that under load work converges into larger, more efficient batches. To bound how
// long a key waits, it flushes a partial batch once maxFlushInterval elapses since the batch began.
type keymapWriter struct {
	// handles fatal DB errors and signals an immediate (panic) shutdown
	errorMonitor *util.ErrorMonitor

	// the keymap that keys are written into
	keymap keymap.Keymap

	// unflushedDataCache holds keys that have been written but not yet persisted to the keymap. The writer
	// prunes a key from it once that key is durable in the keymap. Shared with the disk table (reads happen on
	// caller threads); access is via sync.Map, which is goroutine safe. This is the one piece of shared state
	// the writer touches directly instead of via a channel.
	unflushedDataCache *sync.Map

	// metrics encapsulates DB metrics (may be nil).
	metrics *metrics.LittDBMetrics

	// clock provides the current time, used for metrics.
	clock func() time.Time

	// the name of the table, used for metric labels
	name string

	// bounded channel of work; a full channel backpressures the flush loop (and therefore writes)
	requestChan chan any

	// the maximum number of keys coalesced into a single keymap.Put
	maxBatchSize int

	// the maximum time a batch is allowed to accumulate before it is flushed, even if not full
	maxFlushInterval time.Duration

	// watermarkChan publishes the highest segment index whose keys are all durable in the keymap to the
	// control loop, which owns the authoritative watermark that gates garbage collection. Sends are
	// fire-and-forget (see publishWatermark): the writer must never block on the control loop, or it could
	// deadlock the seal path (control loop -> flush loop -> writer -> control loop).
	watermarkChan chan int64
}

// newKeymapWriter creates a keymap writer. Call run() in a dedicated goroutine to start it. watermarkChan is
// the channel on which the writer publishes its flushed-segment watermark to the control loop.
func newKeymapWriter(
	errorMonitor *util.ErrorMonitor,
	keymap keymap.Keymap,
	unflushedDataCache *sync.Map,
	metrics *metrics.LittDBMetrics,
	clock func() time.Time,
	name string,
	channelSize int,
	maxBatchSize int,
	maxFlushInterval time.Duration,
	watermarkChan chan int64,
) *keymapWriter {

	return &keymapWriter{
		errorMonitor:       errorMonitor,
		keymap:             keymap,
		unflushedDataCache: unflushedDataCache,
		metrics:            metrics,
		clock:              clock,
		name:               name,
		requestChan:        make(chan any, channelSize),
		maxBatchSize:       maxBatchSize,
		maxFlushInterval:   maxFlushInterval,
		watermarkChan:      watermarkChan,
	}
}

// writeBatch writes a batch of keys into the keymap and then prunes them from the unflushed data cache. The
// prune happens only after the keymap Put succeeds, so a key is never absent from both the keymap and the
// unflushed cache. An empty batch is a no-op.
func (w *keymapWriter) writeBatch(keys []*types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}

	if w.metrics != nil {
		start := w.clock()
		defer func() {
			w.metrics.ReportKeymapFlushLatency(w.name, w.clock().Sub(start))
		}()
	}

	if err := w.keymap.Put(keys); err != nil {
		return fmt.Errorf("failed to write keys to keymap: %w", err)
	}

	// The keys are now durable in both the segment and the keymap, so it is safe to drop them from the
	// unflushed data cache.
	for _, key := range keys {
		w.unflushedDataCache.Delete(util.UnsafeBytesToString(key.Key))
	}

	return nil
}

// publishWatermark notifies the control loop that every segment up to and including watermark is now durable
// in the keymap. The send is fire-and-forget and never blocks the writer (blocking here could deadlock the
// seal path, since the control loop may be waiting on the flush loop, which may be waiting on this writer).
// If the channel is full the update is simply dropped: the watermark is monotonic and the control loop drains
// the channel each GC pass, so a later publish delivers an equal-or-newer value. The only consequence of a
// drop is that GC briefly collects a little less, which self-corrects.
func (w *keymapWriter) publishWatermark(watermark int64) {
	select {
	case w.watermarkChan <- watermark:
	default:
	}
}

// scheduleWrite enqueues a batch of newly-durable keys to be written into the keymap. sealedSegment is the index
// of the segment this batch completes, or -1 for a mutable-segment flush. Blocks if the writer's channel is
// full (backpressure); returns an error only if the DB is panicking.
func (w *keymapWriter) scheduleWrite(keys []*types.ScopedKey, sealedSegment int64) error {
	if len(keys) == 0 && sealedSegment < 0 {
		// Nothing to write and no segment to mark complete.
		return nil
	}
	request := &keymapWriteRequest{keys: keys, sealedSegment: sealedSegment}
	return util.Send(w.errorMonitor, w.requestChan, request)
}

// drain asks the writer to apply all queued writes and stop, blocking until it has done so. Used on clean
// shutdown, after the flush loop has stopped scheduling new work.
func (w *keymapWriter) drain() error {
	shutdownCompleteChan := make(chan struct{}, 1)
	request := &keymapWriterShutdownRequest{shutdownCompleteChan: shutdownCompleteChan}
	if err := util.Send(w.errorMonitor, w.requestChan, request); err != nil {
		return fmt.Errorf("failed to send keymap writer shutdown request: %w", err)
	}
	if _, err := util.Await(w.errorMonitor, shutdownCompleteChan); err != nil {
		return fmt.Errorf("failed to await keymap writer drain: %w", err)
	}
	return nil
}

// sync blocks until every write scheduled before this call has been applied to the keymap and the flushed
// watermark advanced accordingly. The writer keeps running afterwards.
func (w *keymapWriter) sync() error {
	doneChan := make(chan struct{}, 1)
	request := &keymapWriterSyncRequest{doneChan: doneChan}
	if err := util.Send(w.errorMonitor, w.requestChan, request); err != nil {
		return fmt.Errorf("failed to send keymap writer sync request: %w", err)
	}
	if _, err := util.Await(w.errorMonitor, doneChan); err != nil {
		return fmt.Errorf("failed to await keymap writer sync: %w", err)
	}
	return nil
}

// run is the writer's event loop. It exits when it receives a shutdown request (clean shutdown, after
// draining all queued work) or when the error monitor signals an immediate shutdown (panic).
func (w *keymapWriter) run() {
	for {
		// Block until there is work to do (or a panic forces an immediate exit).
		var message any
		select {
		case <-w.errorMonitor.ImmediateShutdownRequired():
			return
		case message = <-w.requestChan:
		}

		// A barrier (shutdown or sync) with no batch in progress: all prior work is already applied (FIFO).
		switch barrier := message.(type) {
		case *keymapWriterShutdownRequest:
			barrier.shutdownCompleteChan <- struct{}{}
			return
		case *keymapWriterSyncRequest:
			barrier.doneChan <- struct{}{}
			continue
		}

		// Start a batch with the first request, then accumulate more work into it.
		request := message.(*keymapWriteRequest)
		batch := request.keys
		maxSealed := request.sealedSegment
		barrier, ok := w.accumulate(&batch, &maxSealed)
		if !ok {
			// A panic forced an immediate shutdown; abandon the batch (repair restores it on restart).
			return
		}

		if err := w.writeBatch(batch); err != nil {
			w.errorMonitor.Panic(fmt.Errorf("failed to write keys to keymap: %w", err))
			return
		}

		// Keys arrive in segment order, so once a sealed segment's request has been applied, every key of
		// that segment and every earlier segment is durable in the keymap. Publish the watermark so the
		// control loop may let GC collect those segments. maxSealed is monotonically non-decreasing (FIFO).
		if maxSealed >= 0 {
			w.publishWatermark(maxSealed)
		}

		// Handle the barrier (if any) that ended accumulation, now that the batch has been applied.
		switch b := barrier.(type) {
		case *keymapWriterShutdownRequest:
			b.shutdownCompleteChan <- struct{}{}
			return
		case *keymapWriterSyncRequest:
			b.doneChan <- struct{}{}
		}
	}
}

// accumulate grows the batch with further scheduled work until it reaches maxBatchSize or maxFlushInterval
// elapses since the batch began, whichever comes first. The writer deliberately lets a partial batch build
// up (rather than flushing the instant the channel empties) so that, under load, work coalesces into larger
// and more efficient keymap writes; the interval bounds how long any key waits before being written.
// maxSealed is updated to the highest sealed-segment index seen across the coalesced requests.
//
// Returns a non-nil barrier request (shutdown or sync) if one is received, so the caller can flush the batch
// before acting on it. The boolean is false if a panic-induced shutdown was signalled, in which case the
// writer must abort without flushing.
func (w *keymapWriter) accumulate(batch *[]*types.ScopedKey, maxSealed *int64) (any, bool) {
	timer := time.NewTimer(w.maxFlushInterval)
	defer timer.Stop()

	for len(*batch) < w.maxBatchSize {
		select {
		case <-w.errorMonitor.ImmediateShutdownRequired():
			return nil, false
		case <-timer.C:
			return nil, true
		case message := <-w.requestChan:
			switch m := message.(type) {
			case *keymapWriterShutdownRequest:
				return m, true
			case *keymapWriterSyncRequest:
				return m, true
			case *keymapWriteRequest:
				*batch = append(*batch, m.keys...)
				if m.sealedSegment > *maxSealed {
					*maxSealed = m.sealedSegment
				}
			}
		}
	}
	return nil, true
}
