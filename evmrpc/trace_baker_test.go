package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtracers "github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// fakeTracerAPI drives the baker with controllable per-call results.
type fakeTracerAPI struct {
	mu    sync.Mutex
	calls int32
	// keyed by height
	results map[int64][]*gethtracers.TxTraceResult
	errs    map[int64]error
	// optional: blocks until released, simulates a long bake
	gate chan struct{}
}

func (f *fakeTracerAPI) TraceBlockByNumber(_ context.Context, number rpc.BlockNumber, _ *gethtracers.TraceConfig) ([]*gethtracers.TxTraceResult, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gate != nil {
		<-f.gate
	}
	if err, ok := f.errs[number.Int64()]; ok {
		return nil, err
	}
	return f.results[number.Int64()], nil
}

func waitForCount(t *testing.T, fn func() uint64, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for count >= %d (got %d)", want, fn())
}

func TestTraceBakerBakesAndCaches(t *testing.T) {
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	tx1 := common.HexToHash("0x11")
	tx2 := common.HexToHash("0x22")
	api := &fakeTracerAPI{
		results: map[int64][]*gethtracers.TxTraceResult{
			42: {
				{TxHash: tx1, Result: json.RawMessage(`{"calls":[1]}`)},
				{TxHash: tx2, Result: json.RawMessage(`{"calls":[2]}`)},
			},
		},
	}

	b := NewTraceBaker(nil, cache, TraceBakerConfig{Workers: 1, QueueSize: 8})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	b.Enqueue(42)
	waitForCount(t, b.BakedCount, 1)

	v, ok, err := cache.Get(42, "callTracer", tx1)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"calls":[1]}`, string(v))

	v, ok, err = cache.Get(42, "callTracer", tx2)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"calls":[2]}`, string(v))
}

func TestTraceBakerEnqueueIsNonBlocking(t *testing.T) {
	// QueueSize=1 + a single worker held on the gate. The first Enqueue
	// fills the queue; the second drops without blocking. Consensus
	// latency must never depend on baker progress.
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	gate := make(chan struct{})
	api := &fakeTracerAPI{gate: gate, results: map[int64][]*gethtracers.TxTraceResult{}}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{Workers: 1, QueueSize: 1})
	b.tracersAPI = api
	b.Start()
	defer func() {
		close(gate)
		b.Stop()
	}()

	b.Enqueue(1) // worker picks it up, blocks on gate
	// give the worker a moment to dequeue
	time.Sleep(20 * time.Millisecond)
	b.Enqueue(2)               // sits in the queue
	for i := 0; i < 100; i++ { // any number > buffer must drop
		b.Enqueue(int64(i + 3))
	}
	require.Greater(t, b.DroppedCount(), uint64(0),
		"queue full must drop subsequent Enqueue calls instead of blocking")
}

func TestTraceBakerErrorBecomesFailedCount(t *testing.T) {
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	api := &fakeTracerAPI{
		errs: map[int64]error{99: errors.New("boom")},
	}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{Workers: 1, QueueSize: 8})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	b.Enqueue(99)
	waitForCount(t, b.FailedCount, 1)
	require.Equal(t, uint64(0), b.BakedCount(), "errors should not count as baked")
}

func TestTraceBakerSkipsNilOrErroredTxResults(t *testing.T) {
	// Tracer per-tx errors come back as TxTraceResult{Error:..., Result:nil}.
	// The baker must skip those without crashing or caching empty values.
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	tx := common.HexToHash("0xab")
	api := &fakeTracerAPI{
		results: map[int64][]*gethtracers.TxTraceResult{
			7: {
				nil,
				{TxHash: common.HexToHash("0xff"), Result: nil, Error: "trace failed"},
				{TxHash: tx, Result: json.RawMessage(`{"ok":1}`)},
			},
		},
	}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{Workers: 1, QueueSize: 8})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	b.Enqueue(7)
	waitForCount(t, b.BakedCount, 1)

	v, ok, err := cache.Get(7, "callTracer", tx)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"ok":1}`, string(v))

	_, ok, _ = cache.Get(7, "callTracer", common.HexToHash("0xff"))
	require.False(t, ok, "errored tx should not be cached")
}

func TestTraceBakerMultipleTracers(t *testing.T) {
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	tx := common.HexToHash("0x77")
	api := &fakeTracerAPI{
		results: map[int64][]*gethtracers.TxTraceResult{
			3: {{TxHash: tx, Result: json.RawMessage(`{"v":1}`)}},
		},
	}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{
		Workers:   1,
		QueueSize: 8,
		Tracers:   []string{"callTracer", "prestateTracer"},
	})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	b.Enqueue(3)
	waitForCount(t, b.BakedCount, 2)

	for _, name := range []string{"callTracer", "prestateTracer"} {
		v, ok, err := cache.Get(3, name, tx)
		require.NoError(t, err)
		require.True(t, ok, "tracer %s should be cached", name)
		require.JSONEq(t, `{"v":1}`, string(v))
	}
}

func TestTraceBakerLastBakedHeightAdvances(t *testing.T) {
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	api := &fakeTracerAPI{
		results: map[int64][]*gethtracers.TxTraceResult{
			3: {{TxHash: common.HexToHash("0x1"), Result: json.RawMessage(`{}`)}},
			5: {{TxHash: common.HexToHash("0x2"), Result: json.RawMessage(`{}`)}},
			7: {{TxHash: common.HexToHash("0x3"), Result: json.RawMessage(`{}`)}},
		},
	}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{Workers: 1, QueueSize: 8})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	for _, h := range []int64{3, 5, 7} {
		b.Enqueue(h)
	}
	waitForCount(t, b.BakedCount, 3)

	got, err := cache.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(7), got, "last_baked_height must advance to the highest baked height")
}

func TestTraceBakerCatchUpFromLastBaked(t *testing.T) {
	// Persist last_baked=5; tip=8; baker should bake heights 6, 7, 8.
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()
	require.NoError(t, cache.SetLastBakedHeight(5))

	api := &fakeTracerAPI{
		results: map[int64][]*gethtracers.TxTraceResult{
			6: {{TxHash: common.HexToHash("0x6"), Result: json.RawMessage(`{}`)}},
			7: {{TxHash: common.HexToHash("0x7"), Result: json.RawMessage(`{}`)}},
			8: {{TxHash: common.HexToHash("0x8"), Result: json.RawMessage(`{}`)}},
		},
	}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{
		Workers:   1,
		QueueSize: 8,
		TipFn:     func() int64 { return 8 },
	})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	waitForCount(t, b.BakedCount, 3)
	got, err := cache.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(8), got)
}

func TestTraceBakerCatchUpBoundedByWindow(t *testing.T) {
	// last_baked=5, tip=100, window=10 — catch-up must start from tip-window+1
	// (=91), not from 6, so a long-stopped node doesn't burn forever.
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()
	require.NoError(t, cache.SetLastBakedHeight(5))

	results := map[int64][]*gethtracers.TxTraceResult{}
	for h := int64(1); h <= 100; h++ {
		results[h] = []*gethtracers.TxTraceResult{
			{TxHash: common.BigToHash(big.NewInt(h)), Result: json.RawMessage(`{}`)},
		}
	}
	api := &fakeTracerAPI{results: results}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{
		Workers:      1,
		QueueSize:    8,
		WindowBlocks: 10,
		TipFn:        func() int64 { return 100 },
	})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	// Window=10, tip=100 → catch-up bakes 91..100 (10 blocks).
	waitForCount(t, b.BakedCount, 10)
	require.Less(t, atomic.LoadInt32(&api.calls), int32(20),
		"window-bounded catch-up must not bake the whole 1..100 range")
}

func TestTraceBakerPruneLoopRemovesOldRows(t *testing.T) {
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	for h := int64(1); h <= 5; h++ {
		require.NoError(t, cache.Put(h, "callTracer", common.HexToHash("0xab"), json.RawMessage(`"x"`)))
	}

	api := &fakeTracerAPI{results: map[int64][]*gethtracers.TxTraceResult{}}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{
		Workers:       1,
		QueueSize:     1,
		WindowBlocks:  2,
		TipFn:         func() int64 { return 5 },
		PruneInterval: 25 * time.Millisecond,
	})
	b.tracersAPI = api
	b.Start()
	defer b.Stop()

	// Wait for prune to run at least once. Tip=5, window=2 → cutoff=3 → rows
	// for heights 1 and 2 should be deleted; 3, 4, 5 must remain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, ok1, _ := cache.Get(1, "callTracer", common.HexToHash("0xab"))
		_, ok2, _ := cache.Get(2, "callTracer", common.HexToHash("0xab"))
		if !ok1 && !ok2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, h := range []int64{1, 2} {
		_, ok, err := cache.Get(h, "callTracer", common.HexToHash("0xab"))
		require.NoError(t, err)
		require.False(t, ok, "height %d should be pruned", h)
	}
	for _, h := range []int64{3, 4, 5} {
		_, ok, err := cache.Get(h, "callTracer", common.HexToHash("0xab"))
		require.NoError(t, err)
		require.True(t, ok, "height %d should remain", h)
	}
}

func TestTraceBakerStopDrainsAndCleansUp(t *testing.T) {
	cache, err := keeper.NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer cache.Close()

	api := &fakeTracerAPI{results: map[int64][]*gethtracers.TxTraceResult{}}
	b := NewTraceBaker(nil, cache, TraceBakerConfig{Workers: 2, QueueSize: 4})
	b.tracersAPI = api
	b.Start()
	for i := int64(0); i < 4; i++ {
		b.Enqueue(i)
	}
	// Stop must return after the workers drain — no goroutine leak.
	done := make(chan struct{})
	go func() {
		b.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("baker.Stop() did not return")
	}
}
