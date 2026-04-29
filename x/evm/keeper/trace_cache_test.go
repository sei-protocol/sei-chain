package keeper

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestTraceCachePutGet(t *testing.T) {
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	th := common.HexToHash("0x02")
	val := json.RawMessage(`{"calls":[]}`)

	require.NoError(t, c.Put(100, "callTracer", th, val))

	got, ok, err := c.Get(100, "callTracer", th)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, string(val), string(got))
}

func TestTraceCacheMiss(t *testing.T) {
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	_, ok, err := c.Get(0, "callTracer", common.Hash{})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestTraceCacheKeyDistinctness(t *testing.T) {
	// Different (height, tracer) for the same txHash must round-trip
	// independently — no key collisions across the dimensions in the key.
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	th := common.HexToHash("0xbb")
	require.NoError(t, c.Put(1, "callTracer", th, json.RawMessage(`{"a":1}`)))
	require.NoError(t, c.Put(1, "prestateTracer", th, json.RawMessage(`{"a":2}`)))
	require.NoError(t, c.Put(2, "callTracer", th, json.RawMessage(`{"a":3}`)))

	v, ok, _ := c.Get(1, "callTracer", th)
	require.True(t, ok)
	require.JSONEq(t, `{"a":1}`, string(v))
	v, ok, _ = c.Get(1, "prestateTracer", th)
	require.True(t, ok)
	require.JSONEq(t, `{"a":2}`, string(v))
	v, ok, _ = c.Get(2, "callTracer", th)
	require.True(t, ok)
	require.JSONEq(t, `{"a":3}`, string(v))
}

func TestTraceCachePruneByHeight(t *testing.T) {
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	th := common.HexToHash("0x02")
	for h := int64(1); h <= 5; h++ {
		require.NoError(t, c.Put(h, "callTracer", th, json.RawMessage(`"x"`)))
	}

	require.NoError(t, c.Prune(3))

	for _, h := range []int64{1, 2} {
		_, ok, err := c.Get(h, "callTracer", th)
		require.NoError(t, err)
		require.False(t, ok, "height %d should be pruned", h)
	}
	for _, h := range []int64{3, 4, 5} {
		_, ok, err := c.Get(h, "callTracer", th)
		require.NoError(t, err)
		require.True(t, ok, "height %d should remain", h)
	}
}

func TestTraceCacheNilSafe(t *testing.T) {
	// Methods on nil receiver must no-op so callers can use a single
	// keeper-held *TraceCache field that's nil when the feature is off.
	var c *TraceCache
	require.NoError(t, c.Close())
	require.NoError(t, c.Put(1, "x", common.Hash{}, json.RawMessage(`null`)))
	_, ok, err := c.Get(1, "x", common.Hash{})
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, c.Prune(100))

	c.SetTraceEnqueuer(nil)
	c.Enqueue(42) // must not panic
}

type recordingEnqueuer struct{ heights atomic.Value }

func (r *recordingEnqueuer) Enqueue(h int64) {
	cur, _ := r.heights.Load().([]int64)
	r.heights.Store(append(append([]int64(nil), cur...), h))
}

func TestTraceCacheLastBakedHeight(t *testing.T) {
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	// Initially zero.
	got, err := c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(0), got)

	// Round-trip.
	require.NoError(t, c.SetLastBakedHeight(42))
	got, err = c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(42), got)

	// Atomic-max: lower values must be ignored so out-of-order workers
	// can't roll the watermark backwards.
	require.NoError(t, c.SetLastBakedHeight(10))
	got, err = c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(42), got)

	// Higher value advances it.
	require.NoError(t, c.SetLastBakedHeight(100))
	got, err = c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(100), got)
}

func TestTraceCachePruneSparesMetaKey(t *testing.T) {
	// Prune is a range delete on "ts/..." keys; the meta key lives outside
	// that range and must survive.
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Put(1, "callTracer", common.HexToHash("0x1"), nil))
	require.NoError(t, c.SetLastBakedHeight(10))

	require.NoError(t, c.Prune(1_000_000))

	got, err := c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(10), got, "meta/last_baked_height must survive Prune")
}

func TestTraceCacheLastBakedNilSafe(t *testing.T) {
	var c *TraceCache
	require.NoError(t, c.SetLastBakedHeight(5))
	got, err := c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(0), got)
}

func TestTraceCacheEnqueueForwarding(t *testing.T) {
	c, err := NewTraceCache(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	rec := &recordingEnqueuer{}
	c.SetTraceEnqueuer(rec)

	c.Enqueue(7)
	c.Enqueue(8)

	got, _ := rec.heights.Load().([]int64)
	require.Equal(t, []int64{7, 8}, got)

	// Unregistering must stop forwarding.
	c.SetTraceEnqueuer(nil)
	c.Enqueue(9)
	got, _ = rec.heights.Load().([]int64)
	require.Equal(t, []int64{7, 8}, got)
}
