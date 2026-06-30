package disktable

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// gcManagerRunRequest asks the GC manager to run a single collection pass synchronously and signal when done.
// RunGC uses it to make explicit garbage collection deterministic.
type gcManagerRunRequest struct {
	completionChan chan struct{}
}

// gcManagerShutdownRequest asks the GC manager to stop. Because the manager consumes its channel in FIFO order
// after finishing any in-flight pass, by the time it processes this request no further keymap deletes will be
// scheduled — which is why the control loop stops the GC manager before draining the keymap manager.
type gcManagerShutdownRequest struct {
	shutdownCompleteChan chan struct{}
}

// gcManager performs the expensive half of garbage collection on a dedicated goroutine, off the control loop:
// for each expired sealed segment it reads the segment's keys, evaluates the optional GC filter, durably advances
// the gc-watermark (lowestReadableSegment), and schedules the segment's keymap entries for deletion on the keymap
// manager. Keeping this off the control loop means collection never blocks writes, and the watermark fsync never
// runs on the latency-critical path.
//
// Reclaiming a collected segment's files is left to the control loop (deleteEligibleSegments): file removal
// mutates the segments map, which must stay single-writer. The GC manager never reads that map: the control loop
// hands it each segment over sealedSegmentChan the moment the segment is sealed, and the GC manager keeps its own
// local view (sealedSegments) of the segments it has not yet collected. Progress flows back only through the
// keymap manager's deletion watermark — the GC manager schedules a segment's keymap-entry deletion, and once the
// manager reports those entries durably gone the control loop removes the files. There is no synchronous
// GC-manager -> control-loop call, so the two cannot deadlock at shutdown. Open iterators do not pause any of
// this: an iterator reserves its snapshot segments, so their files survive until it closes even as collection
// and file removal proceed (see handleOpenIteratorRequest).
//
// Correctness invariant: the gc-watermark file must be durable no later than the keymap-entry deletes it guards.
// collectExpiredSegments fsyncs the watermark for a segment before it schedules that segment's delete, which
// (because the keymap delete is applied asynchronously after the enqueue) guarantees barrier-durable before
// delete-durable. If the watermark could lag the deletes across a crash, keymap repair would resurrect
// garbage-collected keys.
type gcManager struct {
	logger       *slog.Logger
	errorMonitor *util.ErrorMonitor

	// sealedSegments is the GC manager's local view of sealed segments it has not yet scheduled for deletion,
	// keyed by segment index. It is seeded at construction with the sealed segments already on disk and grown as
	// the control loop hands over newly-sealed segments on sealedSegmentChan. A segment is dropped from this map
	// once its keymap-entry deletion is scheduled. Only the GC manager goroutine touches it.
	sealedSegments map[uint32]*segment.Segment

	// sealedSegmentChan delivers segments from the control loop the moment they are sealed (the control loop
	// calls registerImmutableSegment from expandSegments). The GC manager owns this channel and drains it into
	// sealedSegments. This is the only way the GC manager learns about segments; it never reads the control
	// loop's segment map.
	sealedSegmentChan chan *segment.Segment

	// keymapManager applies the keymap deletes the GC manager schedules.
	keymapManager *keymapManager

	// ttl is the table's TTL, owned by the GC manager goroutine. It is updated only by draining ttlChan, so it
	// needs no lock and the GC manager needs no reference back to the DiskTable.
	ttl time.Duration

	// ttlChan carries TTL updates pushed by DiskTable.SetTTL (one-way, latest-value; see setTTL). The GC manager
	// drains it into ttl before each collection pass.
	ttlChan chan time.Duration

	clock   func() time.Time
	metrics *metrics.LittDBMetrics
	name    string

	// garbageCollectionPeriod is the period at which a collection pass runs.
	garbageCollectionPeriod time.Duration

	// gcWatermarkFile is the durable record of lowestReadableSegment. The GC manager advances and fsyncs it before
	// scheduling a segment's keymap deletes. Only the GC manager goroutine touches it.
	gcWatermarkFile *GCWatermarkFile

	// gcFilter, if non-nil, is consulted before a TTL-expired segment is deleted. A segment may only be deleted
	// once gcFilter returns true for every key in its key file. Only invoked from the GC manager goroutine.
	gcFilter litt.GCFilter

	// The following three fields form a resumable cursor used by gcFilter scanning. When gcFilter blocks
	// (returns false) on a key, GC stops and remembers its position so the next pass resumes where it left off.
	// The cursor is scoped to a single segment and self-invalidates when the segment under consideration changes.
	gcCursorSegment uint32
	gcCursorKeys    []*types.ScopedKey
	gcCursorIndex   int

	// deletionScheduledIndex is the highest segment index whose keymap-entry deletion the GC manager has
	// scheduled. The next collection pass resumes from just above it. Only the GC manager goroutine touches it.
	deletionScheduledIndex int64

	// requestChan carries synchronous run requests (RunGC) and the shutdown request.
	requestChan chan any
}

// newGCManager creates a GC manager. Call run() in a dedicated goroutine to start it.
func newGCManager(
	logger *slog.Logger,
	errorMonitor *util.ErrorMonitor,
	sealedSegmentChannelSize int,
	initialSealedSegments map[uint32]*segment.Segment,
	keymapManager *keymapManager,
	clock func() time.Time,
	metrics *metrics.LittDBMetrics,
	name string,
	garbageCollectionPeriod time.Duration,
	gcWatermarkFile *GCWatermarkFile,
	gcFilter litt.GCFilter,
	deletionScheduledIndex int64,
	initialTTL time.Duration,
) *gcManager {

	return &gcManager{
		logger:                  logger,
		errorMonitor:            errorMonitor,
		sealedSegments:          initialSealedSegments,
		sealedSegmentChan:       make(chan *segment.Segment, sealedSegmentChannelSize),
		keymapManager:           keymapManager,
		ttl:                     initialTTL,
		ttlChan:                 make(chan time.Duration, 1),
		clock:                   clock,
		metrics:                 metrics,
		name:                    name,
		garbageCollectionPeriod: garbageCollectionPeriod,
		gcWatermarkFile:         gcWatermarkFile,
		gcFilter:                gcFilter,
		deletionScheduledIndex:  deletionScheduledIndex,
		requestChan:             make(chan any, 1),
	}
}

// registerImmutableSegment hands a freshly-sealed (immutable) segment to the GC manager, which adds it to its
// local view of collectable segments. Called by the control loop from expandSegments the moment a segment is
// sealed. Blocks if the hand-off channel is full (brief write backpressure during a long GC pass); returns an
// error only if the DB is shutting down. The GC manager never synchronously waits on the control loop, so this
// cannot deadlock.
//
// The segment is reserved here, before the hand-off, so its files cannot be reclaimed while it sits in the GC
// manager's local view (where collectExpiredSegments may still read its keys via GetKeys). The matching release
// happens once the GC manager is done with the segment (collectExpiredSegments after scheduling its delete, or
// releaseAllSealedSegments on clean shutdown). The control loop still holds the owning reservation here, so
// Reserve cannot fail; a false return signals a broken reservation invariant.
func (g *gcManager) registerImmutableSegment(seg *segment.Segment) error {
	if !seg.Reserve() {
		err := fmt.Errorf("failed to reserve sealed segment %d for gc manager", seg.SegmentIndex())
		g.errorMonitor.Panic(err)
		return err
	}
	select {
	case g.sealedSegmentChan <- seg:
		return nil
	case <-g.errorMonitor.ImmediateShutdownRequired():
		// Hand-off failed: the segment never entered sealedSegments, so release the reservation taken above.
		seg.Release()
		return fmt.Errorf("gc manager shutting down; dropping sealed segment %d", seg.SegmentIndex())
	}
}

// setTTL pushes a new TTL to the GC manager over ttlChan. Latest-value: any pending value is discarded before the
// new one is enqueued, and the send is non-blocking. SetTTL is a configuration call, not expected to be invoked
// concurrently.
func (g *gcManager) setTTL(ttl time.Duration) {
	// Pop out the old TTL in the channel if there is one waiting (deadlock prevention).
	select {
	case <-g.ttlChan:
	default:
	}
	// Set the new TTL.
	select {
	case g.ttlChan <- ttl:
	default:
	}
}

// drainTTL applies any pending TTL update from ttlChan to the goroutine-local ttl. Called on the GC manager
// goroutine before each pass.
func (g *gcManager) drainTTL() {
	select {
	case ttl := <-g.ttlChan:
		g.ttl = ttl
	default:
	}
}

// run is the GC manager's event loop. A tick runs one collection pass; a run request runs a pass synchronously
// (for RunGC); a shutdown request acks and exits. It also exits on an immediate (panic) shutdown.
func (g *gcManager) run() {
	ticker := time.NewTicker(g.garbageCollectionPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-g.errorMonitor.ImmediateShutdownRequired():
			return
		case seg := <-g.sealedSegmentChan:
			// The control loop sealed a segment and handed it over. Absorbing it here (rather than only at the
			// start of a pass) keeps the channel drained between passes, so the control loop rarely blocks on the
			// hand-off.
			g.sealedSegments[seg.SegmentIndex()] = seg
		case msg := <-g.requestChan:
			switch req := msg.(type) {
			case *gcManagerRunRequest:
				g.collectExpiredSegments()
				req.completionChan <- struct{}{}
			case *gcManagerShutdownRequest:
				// Release the reservation on every segment still in the local view before acking, so no
				// reservation leaks. This must complete before the ack: the control loop calls stop()
				// (which awaits this ack) before Drop's single release-per-segment runs, so a leaked
				// reservation here would leave a segment's count above zero and hang Drop forever.
				g.releaseAllSealedSegments()
				req.shutdownCompleteChan <- struct{}{}
				return
			default:
				g.errorMonitor.Panic(fmt.Errorf("unknown gc manager message type %T", msg))
				return
			}
		case <-ticker.C:
			g.collectExpiredSegments()
		}
	}
}

// runOnce asks the GC manager to run a single collection pass and blocks until it completes. Used by RunGC.
func (g *gcManager) runOnce() error {
	req := &gcManagerRunRequest{completionChan: make(chan struct{}, 1)}
	if err := util.Send(g.errorMonitor, g.requestChan, req); err != nil {
		return fmt.Errorf("failed to send gc manager run request: %w", err)
	}
	if _, err := util.Await(g.errorMonitor, req.completionChan); err != nil {
		return fmt.Errorf("failed to await gc manager run: %w", err)
	}
	return nil
}

// stop asks the GC manager to stop and blocks until it has done so. The control loop calls this during shutdown
// before draining the keymap manager, so no keymap delete is scheduled after the drain begins.
func (g *gcManager) stop() error {
	shutdownCompleteChan := make(chan struct{}, 1)
	req := &gcManagerShutdownRequest{shutdownCompleteChan: shutdownCompleteChan}
	if err := util.Send(g.errorMonitor, g.requestChan, req); err != nil {
		return fmt.Errorf("failed to send gc manager shutdown request: %w", err)
	}
	if _, err := util.Await(g.errorMonitor, shutdownCompleteChan); err != nil {
		return fmt.Errorf("failed to await gc manager shutdown: %w", err)
	}
	return nil
}

// collectExpiredSegments runs one collection pass: it walks sealed, TTL-expired segments that have not yet been
// scheduled for deletion, durably advances the gc-watermark past each, and schedules its keymap entries for
// deletion on the keymap manager. A GC filter, if configured, can block a segment (and therefore every later
// one); the resume cursor remembers where to continue on the next pass. The control loop reclaims a segment's
// files later, once the keymap manager reports its entries durably deleted.
func (g *gcManager) collectExpiredSegments() {
	// Open iterators do not block collection: an iterator pins its snapshot segments with reservations, so the
	// control loop can delete their keymap entries and drop them from the live map here while their files
	// survive until the iterator closes. Collection schedules keymap deletes but never deletes files itself, and
	// iterators read segment files directly (never the keymap), so nothing here can corrupt an open iterator.

	// Pick up the latest TTL pushed by SetTTL, then use the goroutine-local value for this pass.
	g.drainTTL()
	ttl := g.ttl
	if ttl.Nanoseconds() <= 0 {
		// No TTL configured: nothing expires.
		return
	}

	start := g.clock()
	if g.metrics != nil {
		defer func() {
			g.metrics.ReportGarbageCollectionLatency(g.name, g.clock().Sub(start))
		}()
	}

	// Absorb any segments the control loop sealed since the last pass. The run() select also drains the channel
	// between passes, but draining here too guarantees this pass (notably an explicit RunGC) sees every segment
	// sealed before it began, regardless of how the select happened to interleave.
	g.drainSealedSegments()

	// Walk the sealed segments the GC manager knows about, in index order, starting just past the highest segment
	// already scheduled for deletion. The local view never holds the mutable segment (only sealed segments are
	// handed over by the control loop) and never holds an already-collected segment (each is dropped once its
	// delete is scheduled), so iterating it directly needs no separate floor or ceiling. A missing index means the
	// next segment has not been sealed yet, which also ends the pass.
	for index := uint32(g.deletionScheduledIndex + 1); ; index++ { //nolint:gosec // watermark >= -1, so index >= 0
		seg, ok := g.sealedSegments[index]
		if !ok {
			// The next segment has not been sealed/handed over yet, or there are none left; stop this pass.
			return
		}
		if !seg.IsSealed() {
			// Defensive: only sealed segments are handed over, so this should never happen.
			return
		}

		if start.Sub(seg.GetSealTime()) < ttl {
			// Not old enough; since segments expire in order, neither is any later one, so stop this pass.
			return
		}

		// Load the segment's keys once and cache them while it remains the segment under consideration. A sealed
		// segment's key file is immutable, so the cache stays valid across passes.
		if g.gcCursorKeys == nil || g.gcCursorSegment != index {
			keys, err := seg.GetKeys()
			if err != nil {
				g.errorMonitor.Panic(fmt.Errorf("failed to get keys: %w", err))
				return
			}
			g.gcCursorKeys = keys
			g.gcCursorSegment = index
			g.gcCursorIndex = 0
		}

		// If a GC filter is configured, the segment may only be deleted once the filter returns true for every
		// key. Walk from where the previous pass left off; if the filter blocks, keep the cursor and stop.
		if g.gcFilter != nil {
			for ; g.gcCursorIndex < len(g.gcCursorKeys); g.gcCursorIndex++ {
				sk := g.gcCursorKeys[g.gcCursorIndex]
				ok, err := g.gcFilter(sk.Key, sk.Kind.IsPrimary())
				if err != nil {
					g.errorMonitor.Panic(fmt.Errorf("gc filter failed: %w", err))
					return
				}
				if !ok {
					// The filter blocks this key, and therefore this and all later segments, for this pass.
					return
				}
			}
		}

		// Durably advance the read barrier past this segment BEFORE scheduling its keymap deletes, so the
		// watermark is durable no later than the deletes it guards. Once the barrier covers a segment, that
		// segment is logically deleted: reads, repair, and reload all skip it.
		if err := g.advanceWatermark(index + 1); err != nil {
			g.errorMonitor.Panic(fmt.Errorf("failed to advance gc-watermark: %w", err))
			return
		}

		// Schedule the segment's keymap entries for deletion. The manager applies it asynchronously and then
		// advances the deletion watermark, which lets the control loop later delete the segment's files.
		if err := g.keymapManager.scheduleDelete(g.gcCursorKeys, int64(index)); err != nil {
			// The only error path is a panic-induced shutdown; nothing more to do.
			return
		}
		g.deletionScheduledIndex = int64(index)

		// This segment is now scheduled for deletion; the GC manager is done with it. Drop it from the local view
		// so the map only ever holds not-yet-collected sealed segments, and release the reservation taken when it
		// was handed over (registerImmutableSegment) or seeded. Releasing here does not delay reclamation: the
		// control loop still holds the owning reservation and only releases it once the keymap delete scheduled
		// just above is durable and the deletion watermark advances past this segment.
		delete(g.sealedSegments, index)
		seg.Release()

		// Reset the cursor for the next segment.
		g.gcCursorKeys = nil
		g.gcCursorIndex = 0
	}
}

// drainSealedSegments absorbs every sealed segment currently waiting on sealedSegmentChan into the local view,
// without blocking. The control loop hands a segment over the moment it is sealed; this is how the GC manager
// learns of segments to consider for collection.
func (g *gcManager) drainSealedSegments() {
	for {
		select {
		case seg := <-g.sealedSegmentChan:
			g.sealedSegments[seg.SegmentIndex()] = seg
		default:
			return
		}
	}
}

// releaseAllSealedSegments releases the GC manager's reservation on every segment it is responsible for and
// clears the local view. Called once on clean shutdown so that no reservation is leaked for a segment that was
// handed over but never scheduled for deletion; without this, Drop's single release-per-segment could not drive
// such a segment's reservation count to zero.
//
// It first drains any segments still buffered on sealedSegmentChan: registerImmutableSegment reserves a segment
// before sending it, so a reservation exists even for segments not yet absorbed into the local view. The control
// loop (the only producer) is blocked awaiting this shutdown's ack, so no further segments can arrive and a
// single drain captures them all.
func (g *gcManager) releaseAllSealedSegments() {
	g.drainSealedSegments()
	for index, seg := range g.sealedSegments {
		seg.Release()
		delete(g.sealedSegments, index)
	}
}

// advanceWatermark durably records lowestReadableSegment in the gc-watermark file (fsynced). This is the only
// place the watermark is materialized: there is no in-memory read barrier. The durable value is consulted only at
// boot, to floor repair/reload (see repairKeymap/reloadKeymap) so a crash between this fsync and the keymap
// deletes cannot resurrect collected keys.
func (g *gcManager) advanceWatermark(lowestReadableSegment uint32) error {
	if err := g.gcWatermarkFile.Update(lowestReadableSegment); err != nil {
		return fmt.Errorf("failed to update gc-watermark file: %w", err)
	}
	return nil
}
