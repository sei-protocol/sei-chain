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

// blockTracer is the subset of *gethtracers.API the baker uses.
type blockTracer interface {
	TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *gethtracers.TraceConfig) ([]*gethtracers.TxTraceResult, error)
}

// TraceBaker re-runs committed blocks through the tracer in background workers
// and writes the JSON to a TraceDB. Enqueue is non-blocking; misses fall
// through to live re-execution.
type TraceBaker struct {
	tracersAPI    blockTracer
	cache         *keeper.TraceDB
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

	dropped, baked, failed uint64 // atomic
}

type TraceBakerConfig struct {
	Workers       int           // re-execution goroutines (default 1)
	QueueSize     int           // bounds in-flight heights (default 4096); drops on full
	Tracers       []string      // tracers to bake per block (default ["callTracer"])
	BakeTimeout   time.Duration // per-(block,tracer) timeout (default 60s)
	TipFn         func() int64  // chain tip; enables catch-up + prune when set
	WindowBlocks  int64         // catch-up cap and rolling prune window (0 disables prune)
	PruneInterval time.Duration // prune tick (default 1m)
}

// StartTraceBakerForDebugAPI wires a baker against api and starts it.
// Returns nil when the keeper has no TraceDB.
func StartTraceBakerForDebugAPI(api *DebugAPI, cfg TraceBakerConfig) *TraceBaker {
	if api == nil {
		return nil
	}
	cache := api.keeper.TraceDB()
	if cache == nil {
		return nil
	}
	b := NewTraceBaker(api.tracersAPI, cache, cfg)
	cache.SetTraceEnqueuer(b)
	b.Start()
	return b
}

func NewTraceBaker(api *gethtracers.API, cache *keeper.TraceDB, cfg TraceBakerConfig) *TraceBaker {
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

// Stop signals goroutines to exit and waits for them to drain. Idempotent.
// Doesn't close b.queue so concurrent Enqueue calls can't panic.
func (b *TraceBaker) Stop() {
	b.closeOnce.Do(func() { close(b.done) })
	b.wg.Wait()
}

// Enqueue is non-blocking; drops on a full queue. Dropped blocks fall through
// to live re-execution at debug_trace time.
func (b *TraceBaker) Enqueue(height int64) {
	if b == nil {
		return
	}
	select {
	case <-b.done:
		return
	case b.queue <- height:
	default:
		d := atomic.AddUint64(&b.dropped, 1)
		if d == 1 || d%256 == 0 {
			bakerLogger.Info("trace baker queue full; dropping height",
				"height", height, "dropped_total", d)
		}
	}
}

func (b *TraceBaker) DroppedCount() uint64 { return atomic.LoadUint64(&b.dropped) }
func (b *TraceBaker) BakedCount() uint64   { return atomic.LoadUint64(&b.baked) }
func (b *TraceBaker) FailedCount() uint64  { return atomic.LoadUint64(&b.failed) }

func (b *TraceBaker) workerLoop() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		case h := <-b.queue:
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
	results, err := b.tracersAPI.TraceBlockByNumber(ctx, rpc.BlockNumber(height), &gethtracers.TraceConfig{Tracer: &tracerName})
	if err != nil {
		atomic.AddUint64(&b.failed, 1)
		bakerLogger.Debug("trace baker block trace failed", "height", height, "tracer", tracer, "err", err)
		return
	}
	for _, r := range results {
		if r == nil || r.Result == nil {
			continue
		}
		bz, err := encodeTraceResult(r.Result)
		if err != nil {
			bakerLogger.Debug("trace baker encode failed", "height", height, "tracer", tracer, "tx", r.TxHash.Hex(), "err", err)
			continue
		}
		if err := b.cache.Put(height, tracer, r.TxHash, bz); err != nil {
			bakerLogger.Debug("trace baker cache put failed", "height", height, "tracer", tracer, "tx", r.TxHash.Hex(), "err", err)
		}
	}
	// Skip empty blocks: json.Marshal(nil) is "null", live path returns [].
	if len(results) > 0 {
		if blockBz, err := json.Marshal(results); err != nil {
			bakerLogger.Debug("trace baker block encode failed", "height", height, "tracer", tracer, "err", err)
		} else if err := b.cache.PutBlock(height, tracer, blockBz); err != nil {
			bakerLogger.Debug("trace baker block put failed", "height", height, "tracer", tracer, "err", err)
		}
	}
	atomic.AddUint64(&b.baked, 1)
	if err := b.cache.SetLastBakedHeight(height); err != nil {
		bakerLogger.Debug("trace baker last_baked update failed", "height", height, "tracer", tracer, "err", err)
	}
}

// catchUpLoop bakes blocks committed since the last successful run, bounded
// by WindowBlocks so a long-stopped node doesn't bake from genesis.
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

// pruneLoop deletes rows older than (tip - WindowBlocks) every PruneInterval.
func (b *TraceBaker) pruneLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			cutoff := b.tipFn() - b.windowBlocks
			if cutoff <= 0 {
				continue
			}
			if err := b.cache.Prune(cutoff); err != nil {
				bakerLogger.Debug("trace baker prune failed", "cutoff", cutoff, "err", err)
			}
		}
	}
}

func encodeTraceResult(v interface{}) (json.RawMessage, error) {
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
