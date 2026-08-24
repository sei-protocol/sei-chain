package flatkv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// hashQueueScrapeInterval is how often the hasher reports its queue depth. Matches the snapshot writer's
// cadence, which is slow enough to cost nothing and fast enough to catch a stall.
const hashQueueScrapeInterval = 10 * time.Second

// ErrBlockHasherClosed is reported (wrapped) by calls that observe the hasher shutting down normally rather
// than failing. Detect it with errors.Is.
var ErrBlockHasherClosed = errors.New("block hasher closed")

// blockHasher computes each committed block's lattice hash off the execution goroutine.
//
// A block is offered once sealed, with reservations held on its own snapshots and on the preceding block's.
// The hasher reads this block's diff and the prior values those keys held, folds them into the running
// lattice hash, finalizes the snapshots with the metadata that describes them, hands the reservations back,
// and publishes the result.
//
// It shares no mutable state with anything: the accumulator below belongs to it alone, everything else
// arrives in a message or was passed at construction, and it takes no lock other than the one guarding its
// own failure latch. That is what keeps it free of the commit goroutine, which holds the store's write lock
// across work that can block.
//
// The chain is sequential — block N's hashes are N-1's plus N's delta — so blocks are hashed strictly in
// order and the pipeline is one block deep. Parallelism is within a block, inside HashCalculator.
//
// It has no recoverable errors. The first internal failure is latched and stops the hasher, and every
// subsequent call reports it.
type blockHasher struct {
	// mu guards fatalErr.
	mu sync.Mutex

	// perDBLtHash is each database's lattice hash root, keyed by database directory name. Derived from the
	// module map for a database this block touched, carried forward for one it did not.
	perDBLtHash map[string]*lthash.LtHash

	// perDBModuleLtHash is each database's per-module lattice hash. The genuinely incremental state: block
	// N's value is N-1's with this block's delta mixed in.
	perDBModuleLtHash map[string]map[string]*lthash.LtHash

	// perDBModuleStats is each database's per-module key and byte totals, accumulated the same way.
	// Consensus-irrelevant, but persisted and validated on load.
	perDBModuleStats map[string]map[string]lthash.ModuleStats

	// ltCalc folds changed values into the hashes, fanning chunks onto its own pool. Passed at
	// construction rather than read off the store, whose field is cleared during teardown.
	ltCalc *lthash.HashCalculator

	// miscPool runs the per-database diff and prior-value reads. Elastic, because those tasks nest a
	// further fan-out of their own.
	miscPool threading.Pool

	// ctx is the context hashing runs under. Cancelled by stop, and by the store's own context.
	ctx context.Context

	// stop cancels ctx, telling the goroutine to finish and releasing anyone waiting on it.
	stop context.CancelFunc

	// messages is the inbound queue. Its capacity bounds how many blocks may sit unfinalized, and so bounds
	// the memory the pipeline costs: an unfinalized snapshot stops every database's flush frontier.
	messages chan any

	// hashes is the outbound stream, one entry per block in block order. Headroom for a consumer that reads
	// later than it commits; a consumer that stops reading entirely will stall the hasher.
	hashes chan BlockHash

	// published is the most recent block's hash, for readers that want the latest rather than the stream.
	// Single writer, so a plain atomic swap is enough.
	published atomic.Pointer[BlockHash]

	// exited is closed once the goroutine has returned.
	exited chan struct{}

	// fatalErr latches the first failure. Nil until something fails.
	fatalErr error
}

// hasherSeed is the accumulated hash state a hasher starts from, read off the databases before the stores
// exist. A hasher built without it would hash its first block against an empty accumulator and produce a
// wrong hash with no error.
type hasherSeed struct {
	// perDBLtHash is each database's persisted lattice hash root.
	perDBLtHash map[string]*lthash.LtHash

	// perDBModuleLtHash is each database's persisted per-module hashes.
	perDBModuleLtHash map[string]map[string]*lthash.LtHash

	// perDBModuleStats is each database's persisted per-module stats.
	perDBModuleStats map[string]map[string]lthash.ModuleStats

	// committed is the hash of the height the store loaded at, published so a reader has an answer before
	// the first block is hashed.
	committed BlockHash
}

// newBlockHasher starts a hasher seeded with the state loaded from disk. Close stops it.
//
// parent is the store's context: cancelling it stops the hasher too, which matters because the store cancels
// its own context during teardown.
func newBlockHasher(
	parent context.Context,
	seed hasherSeed,
	ltCalc *lthash.HashCalculator,
	miscPool threading.Pool,
	queueSize uint32,
	chanSize uint32,
) *blockHasher {
	ctx, stop := context.WithCancel(parent)
	h := &blockHasher{
		perDBLtHash:       seed.perDBLtHash,
		perDBModuleLtHash: seed.perDBModuleLtHash,
		perDBModuleStats:  seed.perDBModuleStats,
		ltCalc:            ltCalc,
		miscPool:          miscPool,
		ctx:               ctx,
		stop:              stop,
		messages:          make(chan any, max(queueSize, 1)),
		hashes:            make(chan BlockHash, max(chanSize, 1)),
		exited:            make(chan struct{}),
	}
	published := seed.committed
	h.published.Store(&published)
	go h.run()
	go h.reportQueueDepth()
	return h
}

// reportQueueDepth samples how many blocks are waiting behind the block being hashed and reports it.
//
// Sampled from outside rather than counted by the producer, because a producer-side gauge goes silent exactly
// when the producer is blocked — which is the case worth seeing.
func (h *blockHasher) reportQueueDepth() {
	ticker := time.NewTicker(hashQueueScrapeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			otelMetrics.HashQueueDepth.Record(h.ctx, int64(len(h.messages)))
		}
	}
}

// Offer hands a sealed block to the hasher.
//
// The caller must hold a reservation on every snapshot passed in, for both blocks; ownership transfers to the
// hasher, which hands them back once it has read what it needs. It blocks when the queue is full, which is
// the pipeline's backpressure.
func (h *blockHasher) Offer(
	version int64,
	current map[string]snapshot.Snapshot,
	previous map[string]snapshot.Snapshot,
	alreadyHave map[string]int64,
) error {
	request := &hashRequest{
		version:     version,
		current:     current,
		previous:    previous,
		alreadyHave: alreadyHave,
	}
	if err := h.enqueue(request); err != nil {
		return errors.Join(
			fmt.Errorf("offer version %d to block hasher: %w", version, err),
			request.release())
	}
	return nil
}

// Flush blocks until the hasher has dealt with every block offered so far, including one it is part way
// through. It reports the latched error if the hasher has failed.
func (h *blockHasher) Flush() error {
	request := newHashFlushRequest()
	if err := h.enqueue(request); err != nil {
		return fmt.Errorf("flush block hasher: %w", err)
	}
	select {
	case <-request.responseChan:
		if err := h.errorIfBricked(); err != nil {
			return fmt.Errorf("flush block hasher: %w", err)
		}
		return nil
	case <-h.ctx.Done():
		return fmt.Errorf("flush block hasher: %w", h.stoppedError())
	}
}

// HashChan returns the stream of block hashes, one per committed block in block order.
func (h *blockHasher) HashChan() <-chan BlockHash {
	return h.hashes
}

// Published returns the most recent block's hash. It is the height the store loaded at until the first block
// has been hashed, and lags the committed version by however far the hasher is behind.
func (h *blockHasher) Published() BlockHash {
	return *h.published.Load()
}

// Seed returns the hasher's accumulated state, describing every block offered before this call. For callers
// that need to read the running hashes — verifying them against a full rescan, or seeding an import's workers
// from them.
func (h *blockHasher) Seed() (hasherSeed, error) {
	request := newHasherSeedRequest()
	if err := h.enqueue(request); err != nil {
		return hasherSeed{}, fmt.Errorf("read block hasher state: %w", err)
	}
	select {
	case seed := <-request.responseChan:
		return seed, nil
	case <-h.ctx.Done():
		return hasherSeed{}, fmt.Errorf("read block hasher state: %w", h.stoppedError())
	}
}

// Reseed replaces the hasher's accumulated state. For callers that have replaced the databases underneath it,
// whose accumulated hashes therefore describe nothing that still exists.
func (h *blockHasher) Reseed(seed hasherSeed) error {
	request := newHasherReseedRequest(seed)
	if err := h.enqueue(request); err != nil {
		return fmt.Errorf("reseed block hasher: %w", err)
	}
	select {
	case <-request.responseChan:
		return nil
	case <-h.ctx.Done():
		return fmt.Errorf("reseed block hasher: %w", h.stoppedError())
	}
}

// seed copies out the accumulated state. Copied rather than handed over, because the caller may hold it while
// the hasher keeps folding blocks into its own.
func (h *blockHasher) seed() hasherSeed {
	perDB := make(map[string]*lthash.LtHash, len(h.perDBLtHash))
	for dir, hash := range h.perDBLtHash {
		perDB[dir] = hash.Clone()
	}
	perModule := make(map[string]map[string]*lthash.LtHash, len(h.perDBModuleLtHash))
	for dir, modules := range h.perDBModuleLtHash {
		perModule[dir] = cloneModuleHashes(modules)
	}
	perStats := make(map[string]map[string]lthash.ModuleStats, len(h.perDBModuleStats))
	for dir, stats := range h.perDBModuleStats {
		perStats[dir] = cloneModuleStats(stats)
	}
	return hasherSeed{
		perDBLtHash:       perDB,
		perDBModuleLtHash: perModule,
		perDBModuleStats:  perStats,
		committed:         *h.published.Load(),
	}
}

// Close stops the hasher and waits for its goroutine to exit. Anything still queued is discarded, finalized
// first so that handing its reservations back cannot brick an engine. Reports the latched error if the hasher
// failed. Idempotent.
func (h *blockHasher) Close() error {
	h.stop()
	// The goroutine closes exited from a deferred call on every exit path, so this cannot strand.
	<-h.exited
	if err := h.errorIfBricked(); err != nil {
		return fmt.Errorf("close block hasher: %w", err)
	}
	return nil
}

// enqueue puts a message on the queue, blocking while the queue is full, and reports why it could not when
// the hasher has stopped instead. Cleaning up after a message it could not deliver belongs to the caller,
// which is the only one that knows whether the message owns anything.
func (h *blockHasher) enqueue(message hasherMessage) error {
	if err := h.errorIfBricked(); err != nil {
		return fmt.Errorf("block hasher failed: %w", err)
	}

	select {
	case h.messages <- message:
		return nil
	case <-h.ctx.Done():
		return fmt.Errorf("enqueue to block hasher: %w", h.stoppedError())
	}
}

// run drains the queue until the hasher is stopped or a block fails to hash.
//
// Blocks are pulled off the queue before they are folded so each one's old-value read runs while the blocks
// ahead of it are still being folded. Folding order is unchanged: strictly the order they arrived.
func (h *blockHasher) run() {
	defer close(h.exited)

	var window []any

	for {
		window = h.fillWindow(window)

		if len(window) == 0 {
			select {
			case <-h.ctx.Done():
				h.finishQueued()
				return
			case message := <-h.messages:
				window = append(window, h.readAhead(message))
			}
			continue
		}

		// Cleared before advancing: reslicing leaves the entry reachable through the backing array, and a
		// dispatched request holds snapshots that have already been handed back.
		message := window[0]
		window[0] = nil
		window = window[1:]

		err := h.dispatch(message)
		if errors.Is(err, ErrBlockHasherClosed) {
			// Stopped part way through a message rather than failing. The block was finalized
			// and its reservations handed back before this point, so only the published hash is
			// lost, and whoever would have read it is why the hasher is stopping.
			h.finishWindow(window)
			h.finishQueued()
			return
		}
		if err != nil {
			h.brick(err)
			// The accumulator now describes nothing that can be trusted, so no further block
			// may have metadata written from it. What is queued is discarded instead.
			h.discardWindow(window)
			h.discardQueued()
			return
		}
	}
}

// fillWindow moves everything already queued into the look-ahead window, starting each block's old-value
// read as it goes.
//
// The window is deliberately unbounded: it is bounded by the queue, and pulling a block in strictly reduces
// what the pipeline holds. A queued block pins the preceding block's snapshots, which stops every database's
// flush frontier and so keeps that block's diffs resident anyway. Reading it turns a reservation plus the
// resident diff into just the values read, and lets the databases start writing again.
func (h *blockHasher) fillWindow(window []any) []any {
	for {
		select {
		case message := <-h.messages:
			window = append(window, h.readAhead(message))
		default:
			return window
		}
	}
}

// readAhead starts the old-value read of a block entering the look-ahead window, and passes any other
// message through untouched.
//
// Best-effort by design: a block the hasher reaches without one reads inline instead, which is what lets
// every shutdown path stay correct without knowing that read-ahead exists.
func (h *blockHasher) readAhead(message any) any {
	request, ok := message.(*hashRequest)
	if !ok {
		return message
	}
	pending := &pendingOldValues{done: make(chan struct{})}
	request.oldValues = pending
	h.miscPool.Submit(func() {
		defer close(pending.done)
		pending.changed, pending.err = changedValuesByStore(h.miscPool, request.current, request.previous)

		// Handed back even when the read failed: a reservation left held stalls its database's flushes
		// indefinitely, and the read's own failure is reported either way.
		releaseErr := request.releasePrevious()
		if pending.err == nil {
			pending.err = releaseErr
		}
	})
	return message
}

// finishWindow deals with the blocks already pulled into the look-ahead window when the hasher stopped.
//
// Same reasoning as finishQueued: a block that was accepted has to be finalized, because its rows are already
// on disk and dropping it would leave the store's bookkeeping describing an earlier block.
func (h *blockHasher) finishWindow(window []any) {
	for i, message := range window {
		err := h.dispatch(message)
		if err == nil || errors.Is(err, ErrBlockHasherClosed) {
			continue
		}
		h.brick(err)
		h.discardWindow(window[i+1:])
		return
	}
}

// discardWindow abandons every message left in the look-ahead window.
func (h *blockHasher) discardWindow(window []any) {
	for _, message := range window {
		h.discardMessage(message)
	}
}

// finishQueued hashes every block still queued when the hasher stopped, and answers anything else so its
// caller is not left waiting.
//
// Stopping is not a reason to drop a block that was accepted. A block's metadata — its hashes, its stats, its
// height — is written when it is finalized here, in the same atomic batch as the rows it describes. Dropping
// it would leave those rows on disk with the store's bookkeeping describing an earlier block, and the
// accumulator a reopened store seeds from would be short a delta it can never recover.
//
// Publishing may fail, because the stream's consumer is usually the reason the hasher is stopping. That costs
// nothing: the hash is on disk by then, and a stopped hasher has no reader left to inform.
func (h *blockHasher) finishQueued() {
	for {
		select {
		case message := <-h.messages:
			err := h.dispatch(message)
			if err == nil || errors.Is(err, ErrBlockHasherClosed) {
				continue
			}
			h.brick(err)
			// Same reasoning as in run: once a block has failed to hash, nothing further may record
			// metadata derived from the accumulator.
			h.discardQueued()
			return
		default:
			return
		}
	}
}

// dispatch routes one queued message.
func (h *blockHasher) dispatch(message any) error {
	switch request := message.(type) {
	case *hashRequest:
		if err := h.hash(request); err != nil {
			return fmt.Errorf("hash version %d: %w", request.version, err)
		}
		return nil
	case *hashFlushRequest:
		request.responseChan <- struct{}{}
		return nil
	case *hasherSeedRequest:
		request.responseChan <- h.seed()
		return nil
	case *hasherReseedRequest:
		h.perDBLtHash = request.seed.perDBLtHash
		h.perDBModuleLtHash = request.seed.perDBModuleLtHash
		h.perDBModuleStats = request.seed.perDBModuleStats
		published := request.seed.committed
		h.published.Store(&published)
		request.responseChan <- struct{}{}
		return nil
	default:
		return fmt.Errorf("unknown block hasher message type %T", message)
	}
}

// hash folds one block into the running lattice hash, records the result on the block's snapshots, hands the
// reservations back, and publishes the hash.
func (h *blockHasher) hash(request *hashRequest) (err error) {
	start := time.Now()
	defer func() {
		otelMetrics.BlockHashLatency.Record(h.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil {
			logger.Error("FlatKV block hashing failed",
				"version", request.version, "elapsed", time.Since(start), "err", err)
		}
	}()

	readStart := time.Now()
	changed, err := request.awaitOldValues(h.miscPool)
	otelMetrics.HashReadOldValuesLatency.Record(h.ctx, secondsSince(readStart),
		metric.WithAttributes(successAttr(err)))
	if err != nil {
		return fmt.Errorf("gather changed values: %w", err)
	}

	foldStart := time.Now()
	result, err := h.ltCalc.Compute(changed, h.perDBLtHash, h.perDBModuleLtHash, h.perDBModuleStats)
	otelMetrics.HashFoldLatency.Record(h.ctx, secondsSince(foldStart),
		metric.WithAttributes(successAttr(err)))
	if err != nil {
		return fmt.Errorf("compute lt hash: %w", err)
	}
	h.perDBLtHash = result.PerDB
	h.perDBModuleLtHash = result.PerModule
	h.perDBModuleStats = result.PerModuleStats

	// Finalizing records the hashes alongside the data they describe, in the same atomic batch, and is what
	// makes the block eligible to flush. It must happen after the fold above and before the hand-back below:
	// releasing the last reservation on an unfinalized snapshot bricks its engine.
	if err := h.finalize(request, result.Global); err != nil {
		return err
	}

	// The reservations are only needed while the reads above happen. Handing them back here rather than at
	// the end lets the databases resume flushing as early as possible.
	if err := request.release(); err != nil {
		return err
	}

	return h.publish(request.version, result.Global)
}

// finalize records the block's metadata on each of its snapshots: a data store gets its own root, per-module
// hashes and stats, the metadata store gets the store-wide root. A store that replay says already reached
// this height records nothing, since its hash already describes a later block — but it still finalizes,
// because finalization is what makes the snapshot flushable.
func (h *blockHasher) finalize(request *hashRequest, global *lthash.LtHash) error {
	for name, snap := range request.current {
		var writes []*proto.KVPair
		switch {
		case request.alreadyHave[name] >= request.version:
		case name == metadataDir:
			writes = encodeGlobalMetadata(request.version, global)
		default:
			writes = encodeLocalMeta(
				request.version,
				h.perDBLtHash[name],
				h.perDBModuleLtHash[name],
				h.perDBModuleStats[name],
			)
		}
		if err := snap.Finalize(writes); err != nil {
			return fmt.Errorf("finalize %s at version %d: %w", name, request.version, err)
		}
	}
	return nil
}

// publish records the block's hash as the latest and puts it on the stream. The stream carries every block, so
// a consumer that stops reading stalls the hasher here — deliberately, since dropping a hash would break the
// one-per-block contract.
func (h *blockHasher) publish(version int64, global *lthash.LtHash) error {
	checksum := global.Checksum()
	perDB := make(map[string][]byte, len(dataDBDirs))
	for _, dir := range dataDBDirs {
		if hash := h.perDBLtHash[dir]; hash != nil {
			dbChecksum := hash.Checksum()
			perDB[dir] = dbChecksum[:]
		}
	}
	blockHash := BlockHash{
		Hash:        checksum[:],
		BlockHeight: version,
		PerDBHashes: perDB,
	}

	h.published.Store(&blockHash)
	otelMetrics.CurrentHashedHeight.Record(h.ctx, version)

	// Delivering the hash is tried on its own first, because a select offering both outlets picks at random
	// among the ready ones — so a stop would drop hashes the stream had room for.
	select {
	case h.hashes <- blockHash:
		return nil
	default:
	}

	select {
	case h.hashes <- blockHash:
		return nil
	case <-h.ctx.Done():
		return fmt.Errorf("publish hash for version %d: %w", version, h.stoppedError())
	}
}

// discardQueued empties the queue, finalizing and then handing back what each request holds and answering
// each flush so its caller is not left waiting. A message enqueued after this has run is stranded, which only
// happens once the hasher has stopped — the stores are closing by then, and closing a store releases
// everything it holds.
func (h *blockHasher) discardQueued() {
	for {
		select {
		case message := <-h.messages:
			h.discardMessage(message)
		default:
			return
		}
	}
}

// discardMessage abandons one message without acting on it, answering anything waiting on a response so its
// caller is not left blocked.
func (h *blockHasher) discardMessage(message any) {
	switch request := message.(type) {
	case *hashRequest:
		h.discard(request)
	case *hashFlushRequest:
		request.responseChan <- struct{}{}
	case *hasherSeedRequest:
		request.responseChan <- h.seed()
	case *hasherReseedRequest:
		request.responseChan <- struct{}{}
	}
}

// discard abandons one queued block without hashing it. Its snapshots are finalized with nothing recorded
// first, because handing back the last reservation on an unfinalized snapshot bricks its engine — and a
// discarded block's data is still in the WAL, so replay recovers it.
func (h *blockHasher) discard(request *hashRequest) {
	for name, snap := range request.current {
		if err := snap.Finalize(nil); err != nil {
			logger.Error("failed to finalize a discarded block's snapshot",
				"version", request.version, "db", name, "err", err)
		}
	}
	if err := request.release(); err != nil {
		logger.Error("failed to hand back reservations of a discarded block",
			"version", request.version, "err", err)
	}
}

// brick latches err as the hasher's fatal error and stops the hasher.
//
// Stopping is what turns the failure into an error rather than a hang: with the goroutine gone nothing drains
// the queue, so a caller blocked on a full queue or waiting on a flush would wait forever.
func (h *blockHasher) brick(err error) {
	h.mu.Lock()
	if h.fatalErr == nil {
		h.fatalErr = err
	}
	h.mu.Unlock()
	h.stop()
}

// errorIfBricked reports the latched error, or nil if the hasher has not failed. The error is returned as
// latched, for whoever propagates it to describe what they were doing.
func (h *blockHasher) errorIfBricked() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fatalErr
}

// stoppedError reports why the hasher is no longer running: the latched error if it failed, otherwise that it
// was closed. Never nil.
func (h *blockHasher) stoppedError() error {
	if err := h.errorIfBricked(); err != nil {
		return fmt.Errorf("block hasher failed: %w", err)
	}
	return ErrBlockHasherClosed
}
