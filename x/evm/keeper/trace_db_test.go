package keeper

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestTraceDBPutGet(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
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

func TestTraceDBMiss(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	_, ok, err := c.Get(0, "callTracer", common.Hash{})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestTraceDBKeyDistinctness(t *testing.T) {
	// Distinct (height, tracer) for the same txHash must not collide.
	c, err := NewTraceDB(t.TempDir())
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

func TestTraceDBPruneByHeight(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
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

func TestTraceDBNilSafe(t *testing.T) {
	// Nil receiver must no-op so callers can hold a single nilable field.
	var c *TraceDB
	require.NoError(t, c.Close())
	require.NoError(t, c.Put(1, "x", common.Hash{}, json.RawMessage(`null`)))
	_, ok, err := c.Get(1, "x", common.Hash{})
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, c.PutBlock(1, "x", json.RawMessage(`null`)))
	_, ok, err = c.GetBlock(1, "x")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, c.Prune(100))

	c.SetTraceEnqueuer(nil)
	c.Enqueue(42) // must not panic
}

func TestTraceDBPutGetBlock(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	val := json.RawMessage(`[{"txHash":"0x1","result":{}}]`)
	require.NoError(t, c.PutBlock(42, "callTracer", val))

	got, ok, err := c.GetBlock(42, "callTracer")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, string(val), string(got))
}

func TestTraceDBBlockKeyDistinctFromTxKey(t *testing.T) {
	// Per-block "tb/" and per-tx "ts/" prefixes must not collide.
	c, err := NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Put(1, "callTracer", common.HexToHash("0x1"), json.RawMessage(`{"a":1}`)))
	require.NoError(t, c.PutBlock(1, "callTracer", json.RawMessage(`[{"a":2}]`)))

	tx, ok, _ := c.Get(1, "callTracer", common.HexToHash("0x1"))
	require.True(t, ok)
	require.JSONEq(t, `{"a":1}`, string(tx))

	blk, ok, _ := c.GetBlock(1, "callTracer")
	require.True(t, ok)
	require.JSONEq(t, `[{"a":2}]`, string(blk))
}

func TestTraceDBPruneCoversBothKeyspaces(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	for h := int64(1); h <= 5; h++ {
		require.NoError(t, c.Put(h, "callTracer", common.HexToHash("0xab"), json.RawMessage(`"x"`)))
		require.NoError(t, c.PutBlock(h, "callTracer", json.RawMessage(`[]`)))
	}
	require.NoError(t, c.Prune(3))

	for _, h := range []int64{1, 2} {
		_, ok, _ := c.Get(h, "callTracer", common.HexToHash("0xab"))
		require.False(t, ok, "tx row at height %d should be pruned", h)
		_, ok, _ = c.GetBlock(h, "callTracer")
		require.False(t, ok, "block row at height %d should be pruned", h)
	}
	for _, h := range []int64{3, 4, 5} {
		_, ok, _ := c.Get(h, "callTracer", common.HexToHash("0xab"))
		require.True(t, ok, "tx row at height %d should remain", h)
		_, ok, _ = c.GetBlock(h, "callTracer")
		require.True(t, ok, "block row at height %d should remain", h)
	}
}

type recordingEnqueuer struct{ heights atomic.Value }

func (r *recordingEnqueuer) Enqueue(h int64) {
	cur, _ := r.heights.Load().([]int64)
	r.heights.Store(append(append([]int64(nil), cur...), h))
}
func (r *recordingEnqueuer) Stop() {}

func TestTraceDBLastBakedHeight(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
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

func TestTraceDBPruneSparesMetaKey(t *testing.T) {
	// Prune covers ts/ and tb/ ranges; meta/ keys must survive.
	c, err := NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Put(1, "callTracer", common.HexToHash("0x1"), nil))
	require.NoError(t, c.SetLastBakedHeight(10))

	require.NoError(t, c.Prune(1_000_000))

	got, err := c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(10), got, "meta/last_baked_height must survive Prune")
}

func TestTraceDBLastBakedNilSafe(t *testing.T) {
	var c *TraceDB
	require.NoError(t, c.SetLastBakedHeight(5))
	got, err := c.LastBakedHeight()
	require.NoError(t, err)
	require.Equal(t, int64(0), got)
}

func TestTraceDBEnqueueForwarding(t *testing.T) {
	c, err := NewTraceDB(t.TempDir())
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
