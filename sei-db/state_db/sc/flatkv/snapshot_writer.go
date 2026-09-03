package flatkv

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// ErrSnapshotWriterClosed is reported (wrapped) by calls that observe the writer shutting down
// normally rather than failing. Detect it with errors.Is.
var ErrSnapshotWriterClosed = errors.New("snapshot writer closed")

// snapshotQueueScrapeInterval is how often the writer reports its queue depth. Matches the cadence the
// view managers sample their own gauges at.
const snapshotQueueScrapeInterval = 10 * time.Second

// SnapshotWriter decides which committed blocks become snapshots and writes them asynchronously, and
// deletes the snapshots a retention cut line has made unnecessary. Its goroutine is the only one that
// mutates the snapshot tree on a live store.
//
// The writer has no recoverable errors. The first internal failure is latched and every subsequent call
// reports it, so a failure that has no caller to fail at the time it happens still stops the node:
// Offer is on the commit path, so the next Commit fails.
type SnapshotWriter struct {
	// mu guards fatalErr and scheduler.
	mu sync.Mutex

	// dir is the flatkv root holding the snapshot directories, the current symlink and the working dir.
	dir string

	// keepRecent is how many snapshots below the newest to retain. Ignored when externalPruning is set.
	keepRecent uint32

	// externalPruning stands this writer's count-based pruning down in favour of the
	// StorageGarbageCollector's by-height retention.
	externalPruning bool

	// interval is how many blocks apart snapshots are taken. 0 disables them. Consulted only while
	// scheduler is nil, the scheduler owning the cadence outright once there is one.
	interval uint32

	// dbs is the handle each database is checkpointed through, keyed by database directory name.
	// Captured when the view managers were opened, and valid until they are closed.
	dbs map[string]types.Checkpointable

	// ctx is the context checkpoint work runs under. Cancelled by stop, and by the store's own context.
	ctx context.Context

	// stop cancels ctx, telling the background goroutine to finish and releasing anyone waiting on it.
	stop context.CancelFunc

	// messages is the queue. Its capacity is how many blocks may pile up behind a snapshot before
	// offering another one blocks, which is the whole of the writer's backpressure.
	messages chan any

	// pruneCutLine holds the retention cut line the StorageGarbageCollector last asked for, until
	// the writer acts on it. Capacity 1: a cut line still waiting is replaced by the next rather
	// than queued behind it, cut lines only ever rising.
	pruneCutLine chan uint64

	// exited is closed once the background goroutine has returned.
	exited chan struct{}

	// phaseTimer breaks down where the writer's goroutine spends its time. Driven only by that
	// goroutine, since a PhaseTimer instance is not safe for concurrent use.
	phaseTimer *metrics.PhaseTimer

	// fatalErr latches the first failure. Nil until something fails.
	fatalErr error

	// scheduler holds this store to the same checkpoint heights as every other store on the node.
	// Nil leaves the store on its own interval.
	scheduler *controller.CheckpointScheduler
}

// newSnapshotWriter starts a writer for the given databases. Close stops it.
//
// queueDepth is how many blocks may pile up behind a snapshot before offering another one blocks. A
// value below 1 is treated as 1.
//
// scheduler may be nil, which leaves the writer on interval.
//
// parent is the store's context: cancelling it stops the writer too, which matters because the store
// cancels its own context during teardown.
func newSnapshotWriter(
	parent context.Context,
	dir string,
	keepRecent uint32,
	externalPruning bool,
	interval uint32,
	queueDepth uint32,
	dbs map[string]types.Checkpointable,
	scheduler *controller.CheckpointScheduler,
) *SnapshotWriter {
	ctx, stop := context.WithCancel(parent)
	w := &SnapshotWriter{
		dir:             dir,
		keepRecent:      keepRecent,
		externalPruning: externalPruning,
		interval:        interval,
		dbs:             dbs,
		ctx:             ctx,
		stop:            stop,
		messages:        make(chan any, max(queueDepth, 1)),
		pruneCutLine:    make(chan uint64, 1),
		exited:          make(chan struct{}),
		phaseTimer:      metrics.NewPhaseTimer(flatkvMeter, "seidb_snapshot_writer"),
		scheduler:       scheduler,
	}
	go w.run()
	go w.reportQueueDepth()
	return w
}

// setCheckpointScheduler hands the writer the schedule it takes its heights from, replacing whatever
// it had. A nil scheduler returns it to its own interval.
func (w *SnapshotWriter) setCheckpointScheduler(scheduler *controller.CheckpointScheduler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scheduler = scheduler
}

// currentCheckpointScheduler returns the schedule in force, or nil when the writer is on interval.
func (w *SnapshotWriter) currentCheckpointScheduler() *controller.CheckpointScheduler {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scheduler
}

// Offer hands a committed block to the writer, which decides if it should be written to disk.
//
// The writer takes its own reservation on every view for as long as it needs one, and releases it
// whether it writes a snapshot, declines to, or fails. The caller only has to hold a reservation of its
// own until this returns, and so does not have to know whether the writer keeps the block past the call.
func (w *SnapshotWriter) Offer(blockView *sview.StoreView) error {
	version := blockView.BlockHeight()
	if err := blockView.Reserve(); err != nil {
		return fmt.Errorf("reserve version %d for snapshot: %w", version, err)
	}

	request := &snapshotRequest{blockView: blockView}
	if err := w.enqueue(request); err != nil {
		return errors.Join(
			fmt.Errorf("offer version %d to snapshot writer: %w", version, err),
			request.release())
	}
	return nil
}

// PruneBelow hands the writer a retention cut line: every snapshot strictly below it may go. It
// returns as soon as the cut line is recorded, without waiting for the deletion, and reports the
// latched error if the writer has failed.
//
// A cut line still waiting when a newer one arrives is replaced rather than queued behind it. Cut
// lines only ever rise, so the newest is the only one that carries information.
func (w *SnapshotWriter) PruneBelow(cutLine uint64) error {
	if err := w.errorIfBricked(); err != nil {
		return fmt.Errorf("snapshot writer failed: %w", err)
	}

	select {
	case <-w.pruneCutLine: // discard the cut line this one supersedes
	default:
	}
	select {
	case w.pruneCutLine <- cutLine:
	default:
		// Only reachable with a second caller, which refilled the cell between the two selects. Its
		// cut line supersedes this one in turn, so dropping this one costs nothing.
	}
	return nil
}

// CloneSnapshot materializes the snapshot at or below targetVersion into destDir, and does not
// return until it is there. A targetVersion of 0 names the active snapshot.
//
// The copy runs on the writer's goroutine, which is also the goroutine that prunes. That is what
// makes the snapshot it resolves still be there when the copy reaches it: done on the caller's
// thread, a prune can delete the directory between the two.
func (w *SnapshotWriter) CloneSnapshot(targetVersion int64, destDir string) error {
	request := newCloneRequest(targetVersion, destDir)
	if err := w.enqueue(request); err != nil {
		return fmt.Errorf("clone snapshot for version %d: %w", targetVersion, err)
	}
	select {
	case err := <-request.responseChan:
		return err
	case <-w.ctx.Done():
		return fmt.Errorf("clone snapshot for version %d: %w", targetVersion, w.stoppedError())
	}
}

// Flush blocks until the writer has dealt with every block offered so far, including a snapshot it is
// part way through. It reports the latched error if the writer has failed.
//
// It is not a barrier for PruneBelow, which delivers on its own channel.
func (w *SnapshotWriter) Flush() error {
	request := newFlushRequest()
	if err := w.enqueue(request); err != nil {
		return fmt.Errorf("flush snapshot writer: %w", err)
	}
	select {
	case <-request.responseChan:
		if err := w.errorIfBricked(); err != nil {
			return fmt.Errorf("flush snapshot writer: %w", err)
		}
		return nil
	case <-w.ctx.Done():
		return fmt.Errorf("flush snapshot writer: %w", w.stoppedError())
	}
}

// Close stops the writer and waits for its goroutine to exit, which may include finishing blocks that
// are still queued. Reports the latched error if the writer failed. Idempotent.
func (w *SnapshotWriter) Close() error {
	w.stop()
	// The goroutine closes exited from a deferred call on every exit path, so this cannot strand.
	<-w.exited
	if err := w.errorIfBricked(); err != nil {
		return fmt.Errorf("close snapshot writer: %w", err)
	}
	return nil
}

// enqueue puts a message on the queue, blocking while the queue is full, and reports why it could not
// when the writer has stopped instead. Cleaning up after a message it could not deliver belongs to the
// caller, which is the only one that knows whether the message owns anything.
func (w *SnapshotWriter) enqueue(message any) error {
	if err := w.errorIfBricked(); err != nil {
		return fmt.Errorf("snapshot writer failed: %w", err)
	}

	select {
	case w.messages <- message:
		return nil
	case <-w.ctx.Done():
		return fmt.Errorf("enqueue to snapshot writer: %w", w.stoppedError())
	}
}

// onSnapshotInterval reports whether a committed block becomes a snapshot on this writer's own
// cadence. Snapshots are taken every interval blocks; an interval of 0 disables them, at the cost of a
// WAL that grows without bound and a restart that replays the whole history.
//
// Only consulted while the writer has no scheduler.
func (w *SnapshotWriter) onSnapshotInterval(version int64) bool {
	if w.interval == 0 || version <= 0 {
		return false
	}
	return version%int64(w.interval) == 0
}

// reportQueueDepth samples how many blocks are waiting behind the snapshot being written and updates
// metrics.
func (w *SnapshotWriter) reportQueueDepth() {
	ticker := time.NewTicker(snapshotQueueScrapeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			otelMetrics.SnapshotQueueDepth.Record(w.ctx, int64(len(w.messages)))
		}
	}
}

// run acts on the blocks offered to the writer and the cut lines handed to it, until the writer is
// stopped or one of them fails.
func (w *SnapshotWriter) run() {
	defer close(w.exited)
	// Whatever is still queued is owed a release, so nothing is left holding a reservation that would
	// stall its database for good.
	defer w.discardQueued()

	for {
		// Charged for however long the queue stays empty, so a writer that is never idle is one the
		// cadence is outrunning.
		w.phaseTimer.SetPhase("idle")

		var err error
		select {
		case <-w.ctx.Done():
			return
		case cutLine := <-w.pruneCutLine:
			err = w.handlePruneCutLine(cutLine)
		case message := <-w.messages:
			err = w.handleMessage(message)
		}
		if err != nil {
			w.brick(err)
			return
		}
	}
}

// handleMessage acts on one message from the queue.
func (w *SnapshotWriter) handleMessage(message any) error {
	switch request := message.(type) {
	case *snapshotRequest:
		return w.maybeCheckpointBlock(request)
	case *cloneRequest:
		// Answered rather than returned: a clone that fails describes one caller's read, not the
		// writer's ability to keep snapshotting, so it must not stop the writer the way a failed
		// checkpoint does.
		request.responseChan <- w.cloneSnapshot(request)
		return nil
	case *flushRequest:
		request.responseChan <- struct{}{}
		return nil
	default:
		return fmt.Errorf("unknown snapshot writer message type %T", message)
	}
}

// cloneSnapshot resolves the snapshot a clone request names and copies it into the request's
// destination directory.
func (w *SnapshotWriter) cloneSnapshot(request *cloneRequest) error {
	w.phaseTimer.SetPhase("clone_snapshot")

	snapDir, err := resolveSnapshotToClone(w.dir, request.targetVersion)
	if err != nil {
		return err
	}
	if err := createWorkingDir(snapDir, request.destDir); err != nil {
		return fmt.Errorf("clone snapshot %s: %w", filepath.Base(snapDir), err)
	}
	return nil
}

// handlePruneCutLine deletes the snapshots a retention cut line from the StorageGarbageCollector has
// made unnecessary.
//
// A failure here stops the writer, as a failed checkpoint does. A snapshot that will not unlink means
// the storage underneath is misconfigured or broken, and a node that cannot reclaim snapshots is on a
// terminal path to a full disk, so there is nothing to recover to.
func (w *SnapshotWriter) handlePruneCutLine(cutLine uint64) error {
	w.phaseTimer.SetPhase("prune_snapshots")
	if err := pruneSnapshotsBelow(w.ctx, w.dir, cutLine); err != nil {
		return fmt.Errorf("prune snapshots below cut line %d: %w", cutLine, err)
	}
	return nil
}

// Possibly checkpoint a block. Releases reservation when finished regardless of choice.
//
// Every committed block reaches here, so the schedule is asked about every one of them rather than
// only about the heights an interval would have offered it. That is what lets one schedule hold
// several stores to the same height: a height it picks for another store is a height this one is
// asked about.
func (w *SnapshotWriter) maybeCheckpointBlock(request *snapshotRequest) (err error) {
	// The only release for a block that reached the goroutine, covering written, declined and failed
	// alike. A reservation left held stalls its view manager's flushes indefinitely.
	defer func() {
		if relErr := request.release(); relErr != nil {
			err = errors.Join(err, fmt.Errorf(
				"release reservations for version %d: %w", request.blockView.BlockHeight(), relErr))
		}
	}()

	scheduler := w.currentCheckpointScheduler()
	if scheduler == nil {
		if !w.onSnapshotInterval(request.blockView.BlockHeight()) {
			w.phaseTimer.SetPhase("release_declined_block")
			return nil
		}
		return w.checkpointBlock(request)
	}

	if !scheduler.ShouldCheckpoint(checkpointStoreName, request.blockView.BlockHeight()) {
		w.phaseTimer.SetPhase("release_declined_block")
		return nil
	}
	// The schedule holds this height until it is reported, so it has to be reported on the failure
	// path as much as on the success one: an unreported height stops the node checkpointing.
	defer scheduler.MarkCheckpointComplete(checkpointStoreName, request.blockView.BlockHeight())
	return w.checkpointBlock(request)
}

// checkpointBlock writes the snapshot for a block the cadence selected.
func (w *SnapshotWriter) checkpointBlock(request *snapshotRequest) error {
	if err := w.writeCheckpoint(request); err != nil {
		return fmt.Errorf("write snapshot at version %d: %w", request.blockView.BlockHeight(), err)
	}
	return nil
}

// discardQueued empties the queue, handing back what each snapshot request holds and answering each
// flush so its caller is not left waiting. A message enqueued after this has run is stranded, which
// only happens once the writer has stopped — the view managers are closing by then, and closing one
// releases everything it holds.
func (w *SnapshotWriter) discardQueued() {
	for {
		select {
		case message := <-w.messages:
			switch request := message.(type) {
			case *snapshotRequest:
				if err := request.release(); err != nil {
					logger.Error("failed to release reservations of a discarded snapshot",
						"version", request.blockView.BlockHeight(), "err", err)
				}
			case *cloneRequest:
				request.responseChan <- fmt.Errorf("clone snapshot for version %d: %w",
					request.targetVersion, w.stoppedError())
			case *flushRequest:
				request.responseChan <- struct{}{}
			}
		default:
			return
		}
	}
}

// writeCheckpoint writes one snapshot: the databases are copied while they are pinned, and the copy is published
// as the active snapshot. It records how long that took, and reports a failure that by then has no
// caller to return to.
func (w *SnapshotWriter) writeCheckpoint(request *snapshotRequest) (err error) {
	start := time.Now()
	defer func() {
		otelMetrics.SnapshotWriteLatency.Record(w.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil {
			logger.Error("FlatKV snapshot failed",
				"version", request.blockView.BlockHeight(), "elapsed", time.Since(start), "err", err)
		}
	}()

	// Work already under way is not abandoned when the writer is told to stop. w.ctx is cancelled to
	// release callers blocked on the queue, but Close is documented to let an in-flight snapshot finish,
	// and the databases it is reading are closed only after the drain. Handing it a cancellable context
	// would instead abort its AwaitFlush and brick the writer on the way out.
	workCtx := context.WithoutCancel(w.ctx)

	tmpPath, err := checkpointDatabases(
		workCtx, w.dir, request.blockView, w.dbs, w.phaseTimer)
	if err != nil {
		return fmt.Errorf("snapshot version %d: %w", request.blockView.BlockHeight(), err)
	}

	w.phaseTimer.SetPhase("publish_snapshot")
	pruned, err := publishSnapshot(
		workCtx, w.dir, w.keepRecent, w.externalPruning, request.blockView.BlockHeight(), tmpPath)
	if err != nil {
		return fmt.Errorf("publish snapshot at version %d: %w", request.blockView.BlockHeight(), err)
	}

	otelMetrics.CurrentSnapshotHeight.Record(w.ctx, request.blockView.BlockHeight())
	logger.Info("FlatKV snapshot created",
		"version", request.blockView.BlockHeight(), "pruned", pruned, "elapsed", time.Since(start))
	return nil
}

// brick latches err as the writer's fatal error and stops the writer.
//
// Stopping is what turns the failure into an error rather than a hang: with the goroutine gone nothing
// drains the queue, so a caller blocked on a full queue or waiting on a flush would wait forever.
// Cancelling the context releases them to read the latched error instead.
func (w *SnapshotWriter) brick(err error) {
	w.mu.Lock()
	if w.fatalErr == nil {
		w.fatalErr = err
	}
	w.mu.Unlock()
	w.stop()
}

// errorIfBricked reports the latched error, or nil if the writer has not failed. The error is returned
// as latched, for whoever propagates it to describe what they were doing.
func (w *SnapshotWriter) errorIfBricked() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fatalErr
}

// stoppedError reports why the writer is no longer running: the latched error if it failed, otherwise
// that it was closed. Never nil.
func (w *SnapshotWriter) stoppedError() error {
	if err := w.errorIfBricked(); err != nil {
		return fmt.Errorf("snapshot writer failed: %w", err)
	}
	return ErrSnapshotWriterClosed
}
