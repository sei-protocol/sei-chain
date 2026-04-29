package evmrpc

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	gethtracers "github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

var bakerLogger = seilog.NewLogger("evmrpc", "trace-baker")

// blockTracer is the subset of *gethtracers.API the baker uses; the
// indirection lets tests drive the worker without standing up a real EVM.
type blockTracer interface {
	TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *gethtracers.TraceConfig) ([]*gethtracers.TxTraceResult, error)
}

// TraceBaker re-runs newly committed blocks through the tracer in worker
// goroutines off the consensus path and stores the JSON output into a
// TraceCache. debug_trace* RPCs hit the cache first; on miss they fall
// through to today's on-demand re-execution. Consensus latency is
// unaffected because Enqueue is a non-blocking channel send and all
// re-execution happens on baker goroutines.
type TraceBaker struct {
	tracersAPI    blockTracer
	cache         *keeper.TraceCache
	tracers       []string
	bakeTimeout   time.Duration
	tipFn         func() int64
	windowBlocks  int64
	pruneInterval time.Duration

	queue   chan int64
	workers int

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup

	dropped uint64 // atomic
	baked   uint64 // atomic
	failed  uint64 // atomic
}

// TraceBakerConfig holds tunable knobs for the baker.
type TraceBakerConfig struct {
	// Workers is the number of re-execution goroutines. Default 1.
	Workers int
	// QueueSize bounds in-flight heights. Default 4096. Drops on full.
	QueueSize int
	// Tracers names the tracers to bake per block. Default ["callTracer"].
	Tracers []string
	// BakeTimeout caps re-execution per block per tracer. Default 60s.
	BakeTimeout time.Duration
	// TipFn returns the current chain tip; used by catch-up and prune.
	// Optional — when nil, both features are skipped.
	TipFn func() int64
	// WindowBlocks bounds catch-up backfill and the rolling prune window.
	// 0 disables prune; catch-up still runs from last_baked+1 to tip.
	WindowBlocks int64
	// PruneInterval is the tick for the prune goroutine. Default 1m.
	PruneInterval time.Duration
}

// StartTraceBakerForDebugAPI wires a TraceBaker against the given DebugAPI's
// tracer surface, registers it on the keeper's TraceCache so EndBlock-driven
// Enqueue calls reach it, and starts the workers. Returns nil if the keeper
// has no TraceCache (the feature is off).
func StartTraceBakerForDebugAPI(api *DebugAPI, cfg TraceBakerConfig) *TraceBaker {
	if api == nil {
		return nil
	}
	cache := api.keeper.TraceCache()
	if cache == nil {
		return nil
	}
	b := NewTraceBaker(api.tracersAPI, cache, cfg)
	cache.SetTraceEnqueuer(b)
	b.Start()
	return b
}

// NewTraceBaker constructs a baker. Call Start to launch workers.
func NewTraceBaker(api *gethtracers.API, cache *keeper.TraceCache, cfg TraceBakerConfig) *TraceBaker {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 4096
	}
	if len(cfg.Tracers) == 0 {
		cfg.Tracers = []string{"callTracer"}
	}
	if cfg.BakeTimeout <= 0 {
		cfg.BakeTimeout = 60 * time.Second
	}
	if cfg.PruneInterval <= 0 {
		cfg.PruneInterval = time.Minute
	}
	return &TraceBaker{
		tracersAPI:    api,
		cache:         cache,
		tracers:       append([]string(nil), cfg.Tracers...),
		bakeTimeout:   cfg.BakeTimeout,
		tipFn:         cfg.TipFn,
		windowBlocks:  cfg.WindowBlocks,
		pruneInterval: cfg.PruneInterval,
		queue:         make(chan int64, cfg.QueueSize),
		workers:       cfg.Workers,
		done:          make(chan struct{}),
	}
}

// Start launches the worker goroutines plus, when TipFn is set, a one-shot
// catch-up sweep (from last_baked+1 up to current tip, bounded by
// WindowBlocks) and a periodic prune ticker (when WindowBlocks > 0).
func (b *TraceBaker) Start() {
	bakerLogger.Info("trace baker starting",
		"workers", b.workers, "queue_size", cap(b.queue),
		"tracers", b.tracers, "window_blocks", b.windowBlocks)
	for i := 0; i < b.workers; i++ {
		b.wg.Add(1)
		go b.workerLoop()
	}
	if b.tipFn != nil {
		b.wg.Add(1)
		go b.catchUpLoop()
		if b.windowBlocks > 0 {
			b.wg.Add(1)
			go b.pruneLoop()
		}
	}
}

// Stop signals workers to drain and exit; blocks until they do.
func (b *TraceBaker) Stop() {
	b.closeOnce.Do(func() {
		close(b.done)
		close(b.queue)
	})
	b.wg.Wait()
}

// Enqueue forwards a height to the worker queue. Non-blocking by design:
// when the queue is full the height is dropped and the corresponding block
// falls through to on-demand re-execution at debug_trace time. Consensus
// latency is unaffected.
func (b *TraceBaker) Enqueue(height int64) {
	if b == nil {
		return
	}
	select {
	case b.queue <- height:
	default:
		d := atomic.AddUint64(&b.dropped, 1)
		// Log sparsely so a stuck baker doesn't flood the journal.
		if d == 1 || d%256 == 0 {
			bakerLogger.Info("trace baker queue full; dropping height",
				"height", height, "dropped_total", d)
		}
	}
}

// DroppedCount returns the cumulative dropped-enqueue count since startup.
func (b *TraceBaker) DroppedCount() uint64 { return atomic.LoadUint64(&b.dropped) }

// BakedCount returns the cumulative successful (block, tracer) bake count.
func (b *TraceBaker) BakedCount() uint64 { return atomic.LoadUint64(&b.baked) }

// FailedCount returns the cumulative failed (block, tracer) bake count.
func (b *TraceBaker) FailedCount() uint64 { return atomic.LoadUint64(&b.failed) }

func (b *TraceBaker) workerLoop() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		case h, ok := <-b.queue:
			if !ok {
				return
			}
			b.bakeBlock(h)
		}
	}
}

func (b *TraceBaker) bakeBlock(height int64) {
	defer func() {
		if r := recover(); r != nil {
			bakerLogger.Error("trace baker panic", "height", height, "panic", r)
		}
	}()
	for _, name := range b.tracers {
		b.bakeBlockOneTracer(height, name)
	}
}

func (b *TraceBaker) bakeBlockOneTracer(height int64, tracer string) {
	ctx, cancel := context.WithTimeout(context.Background(), b.bakeTimeout)
	defer cancel()

	tracerName := tracer
	cfg := &gethtracers.TraceConfig{Tracer: &tracerName}
	results, err := b.tracersAPI.TraceBlockByNumber(ctx, rpc.BlockNumber(height), cfg)
	if err != nil {
		atomic.AddUint64(&b.failed, 1)
		bakerLogger.Debug("trace baker block trace failed",
			"height", height, "tracer", tracer, "err", err)
		return
	}
	for _, r := range results {
		if r == nil || r.Result == nil {
			continue
		}
		bz, err := encodeTraceResult(r.Result)
		if err != nil {
			bakerLogger.Debug("trace baker encode failed",
				"height", height, "tracer", tracer, "tx", r.TxHash.Hex(), "err", err)
			continue
		}
		if err := b.cache.Put(height, tracer, r.TxHash, bz); err != nil {
			bakerLogger.Debug("trace baker cache put failed",
				"height", height, "tracer", tracer, "tx", r.TxHash.Hex(), "err", err)
			continue
		}
	}
	atomic.AddUint64(&b.baked, 1)
	if err := b.cache.SetLastBakedHeight(height); err != nil {
		bakerLogger.Debug("trace baker last_baked update failed",
			"height", height, "tracer", tracer, "err", err)
	}
}

// catchUpLoop bakes any blocks committed since the last successful run.
// Bounded by WindowBlocks so a long-stopped node doesn't try to bake from
// genesis. Exits as soon as it reaches the current tip.
func (b *TraceBaker) catchUpLoop() {
	defer b.wg.Done()
	last, err := b.cache.LastBakedHeight()
	if err != nil || last <= 0 {
		return
	}
	tip := b.tipFn()
	if tip <= last {
		return
	}
	from := last + 1
	if b.windowBlocks > 0 && from < tip-b.windowBlocks+1 {
		from = tip - b.windowBlocks + 1
	}
	bakerLogger.Info("trace baker catch-up", "from", from, "to", tip)
	for h := from; h <= tip; h++ {
		select {
		case <-b.done:
			return
		default:
		}
		b.bakeBlock(h)
	}
}

// pruneLoop ticks every PruneInterval and deletes cache rows older than
// (tip - WindowBlocks). One DeleteRange per tick — cheap on pebble.
func (b *TraceBaker) pruneLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			tip := b.tipFn()
			cutoff := tip - b.windowBlocks
			if cutoff <= 0 {
				continue
			}
			if err := b.cache.Prune(cutoff); err != nil {
				bakerLogger.Debug("trace baker prune failed", "cutoff", cutoff, "err", err)
			}
		}
	}
}

// encodeTraceResult turns a tracer result (either json.RawMessage already,
// or any json-marshalable value) into bytes for the cache. The geth call
// tracer returns json.RawMessage directly; struct/native tracers return
// typed structs.
func encodeTraceResult(v interface{}) (json.RawMessage, error) {
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
