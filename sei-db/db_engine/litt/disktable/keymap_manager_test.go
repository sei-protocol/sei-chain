package disktable

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// buildTestKeymapManager builds a keymapManager backed by an in-memory keymap WITHOUT starting its run loop,
// so a test can drive its methods directly. The returned watermark channel is buffered large enough that no
// publish is dropped during a test.
func buildTestKeymapManager(
	t *testing.T,
	maxBatchSize int,
	deleteBatchSize uint64,
	maxBufferedDeletes uint64,
	channelSize int,
) (*keymapManager, chan int64) {

	em := util.NewErrorMonitor(context.Background(), slog.Default(), nil)
	km, _, err := keymap.NewMemKeymap(slog.Default(), "", false)
	require.NoError(t, err)

	var cache sync.Map
	m := newKeymapManager(
		em,
		km,
		&cache,
		nil, // metrics
		time.Now,
		"test",
		channelSize,
		maxBatchSize,
		math.MaxUint64, // disable the byte-based batch trigger; these tests exercise key-count/time batching
		deleteBatchSize,
		time.Second,
		maxBufferedDeletes,
	)
	// The manager reports deletion watermarks via controlLoop.publishDeletionWatermark; attach a minimal control
	// loop that owns the watermark channel so the tests can observe what the manager publishes. Set before run().
	wmChan := make(chan int64, 1024)
	m.controlLoop = &controlLoop{deletionWatermarkChan: wmChan}
	return m, wmChan
}

// newTestKeymapManager builds a keymapManager and starts its run loop in a goroutine.
func newTestKeymapManager(
	t *testing.T,
	maxBatchSize int,
	deleteBatchSize uint64,
	maxBufferedDeletes uint64,
	channelSize int,
) (*keymapManager, chan int64) {

	m, wmChan := buildTestKeymapManager(t, maxBatchSize, deleteBatchSize, maxBufferedDeletes, channelSize)
	go m.run()
	return m, wmChan
}

// scopedKeys builds ScopedKeys for the given key strings. The address is irrelevant to these tests, which
// only observe key presence/absence in the keymap.
func scopedKeys(keys ...string) []*types.ScopedKey {
	out := make([]*types.ScopedKey, len(keys))
	for i, k := range keys {
		out[i] = &types.ScopedKey{Key: []byte(k), Address: types.NewAddress(0, 0, 0, 0)}
	}
	return out
}

func assertPresent(t *testing.T, m *keymapManager, keys ...string) {
	for _, k := range keys {
		_, ok, err := m.keymap.Get([]byte(k))
		require.NoError(t, err)
		require.True(t, ok, "expected key %q to be present", k)
	}
}

func assertAbsent(t *testing.T, m *keymapManager, keys ...string) {
	for _, k := range keys {
		_, ok, err := m.keymap.Get([]byte(k))
		require.NoError(t, err)
		require.False(t, ok, "expected key %q to be absent", k)
	}
}

// drainWatermark returns the highest deletion watermark published so far (-1 if none).
func drainWatermark(ch chan int64) int64 {
	highest := int64(-1)
	for {
		select {
		case v := <-ch:
			if v > highest {
				highest = v
			}
		default:
			return highest
		}
	}
}

// TestKeymapManagerPutThenDeleteResolvesToDeleted pins the ordering guarantee: a put enqueued before a delete
// is applied before that delete, so a put(K) followed by delete(K) resolves to deleted — even when the delete
// group is split across sub-batches (deleteBatchSize = 1 here). Keys not in the delete group survive.
func TestKeymapManagerPutThenDeleteResolvesToDeleted(t *testing.T) {
	m, wmChan := newTestKeymapManager(t, 1024, 1 /* split every delete key */, 1_000_000, 1024)

	require.NoError(t, m.scheduleWrite(scopedKeys("K", "A", "B", "X", "Y"), 0))
	// Delete group for segment 5 contains K, X, Y (enqueued after the put of the same keys).
	require.NoError(t, m.scheduleDelete(scopedKeys("X", "K", "Y"), 5))
	require.NoError(t, m.drain())

	assertAbsent(t, m, "K", "X", "Y")
	assertPresent(t, m, "A", "B")
	require.Equal(t, int64(5), drainWatermark(wmChan))
}

// TestKeymapManagerDrainsBacklogAndAdvancesWatermark verifies a multi-segment delete backlog is fully applied
// and that the deletion watermark advances to the highest fully-deleted segment.
func TestKeymapManagerDrainsBacklogAndAdvancesWatermark(t *testing.T) {
	m, wmChan := newTestKeymapManager(t, 1024, 2 /* force splitting */, 1_000_000, 1024)

	require.NoError(t, m.scheduleWrite(scopedKeys("a1", "a2", "a3", "b1", "b2", "c1"), 0))
	require.NoError(t, m.scheduleDelete(scopedKeys("a1", "a2", "a3"), 1))
	require.NoError(t, m.scheduleDelete(scopedKeys("b1", "b2"), 2))
	require.NoError(t, m.scheduleDelete(scopedKeys("c1"), 3))
	require.NoError(t, m.drain())

	assertAbsent(t, m, "a1", "a2", "a3", "b1", "b2", "c1")
	require.Equal(t, int64(3), drainWatermark(wmChan))
}

// TestKeymapManagerCoalesceStopsAtDeleteHighWater is a regression test: coalesce must stop ingesting when the
// delete backlog reaches its high-water mark, even when more delete requests are immediately available on the
// channel. Without this bound a producer that keeps the channel non-empty would make coalesce loop forever,
// growing the backlog without limit (puts alone bound the loop, and deletes do not count toward that bound).
// The manager is not started here; coalesce is driven directly against a pre-filled channel.
func TestKeymapManagerCoalesceStopsAtDeleteHighWater(t *testing.T) {
	// Huge put cap so only the delete high-water mark can stop coalesce; small maxBufferedDeletes (10).
	m, _ := buildTestKeymapManager(t, 1_000_000, 4, 10, 128)

	const requests = 50
	for i := 0; i < requests; i++ {
		ks := scopedKeys(fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i), fmt.Sprintf("c%d", i))
		require.NoError(t, m.scheduleDelete(ks, int64(i)))
	}

	require.False(t, m.coalesce())
	require.True(t, m.backpressure, "coalesce must engage backpressure at the high-water mark")
	require.LessOrEqual(t, m.bufferedDeleteCount, uint64(12), "coalesce must stop ingesting near the high-water mark")
	require.NotEmpty(t, m.requestChan, "coalesce must leave remaining requests on the channel, not drain them all")
}

// TestKeymapManagerBackpressureDoesNotDeadlock drives the high-water backpressure path: each delete group
// (10 keys) exceeds maxBufferedDeletes (4), so the manager repeatedly stops popping the (size-2) channel and
// drains to half before resuming, while producers block on the full channel. A deadlock would hang the test;
// instead all work must complete, every key end deleted, and the watermark reach the last segment.
func TestKeymapManagerBackpressureDoesNotDeadlock(t *testing.T) {
	m, wmChan := newTestKeymapManager(t, 8, 2 /* deleteBatchSize */, 4 /* maxBufferedDeletes */, 2 /* channel */)

	const groups = 20
	var allKeys []string
	for g := 0; g < groups; g++ {
		var ks []string
		for i := 0; i < 10; i++ {
			ks = append(ks, fmt.Sprintf("g%d_k%d", g, i))
		}
		require.NoError(t, m.scheduleWrite(scopedKeys(ks...), 0))
		require.NoError(t, m.scheduleDelete(scopedKeys(ks...), int64(g)))
		allKeys = append(allKeys, ks...)
	}
	require.NoError(t, m.drain())

	assertAbsent(t, m, allKeys...)
	require.Equal(t, int64(groups-1), drainWatermark(wmChan))
}

// TestKeymapManagerSyncAppliesThenContinues verifies the sync barrier applies all prior work (advancing the
// watermark) without stopping the manager, so subsequent work still applies.
func TestKeymapManagerSyncAppliesThenContinues(t *testing.T) {
	m, _ := newTestKeymapManager(t, 1024, 1024, 1_000_000, 1024)

	require.NoError(t, m.scheduleWrite(scopedKeys("k1"), 0))
	require.NoError(t, m.scheduleDelete(scopedKeys("k1"), 1))
	require.NoError(t, m.sync())
	assertAbsent(t, m, "k1")

	require.NoError(t, m.scheduleWrite(scopedKeys("k2"), 0))
	require.NoError(t, m.sync())
	assertPresent(t, m, "k2")

	require.NoError(t, m.drain())
}

// TestKeymapManagerPendingPutBytesTracksRawSize pins the cache-bound accounting: pendingPutBytes must reflect the
// raw (pre-compression) value bytes carried on the write request, NOT Address.ValueSize() (which is the compressed
// on-disk size for a compressed table). This is what keeps maxBatchBytes bounding the raw unflushed-data cache.
func TestKeymapManagerPendingPutBytesTracksRawSize(t *testing.T) {
	m, _ := buildTestKeymapManager(t, 1024, 1, 1_000_000, 1024)

	// Address encodes a small (compressed) on-disk size; the request carries a much larger raw size. The
	// accounting must follow the raw size, proving it is decoupled from Address.ValueSize().
	keys := []*types.ScopedKey{
		{Key: []byte("k"), Address: types.NewAddress(0, 0, 0, 10), Kind: types.KeyKindStandalone},
	}
	const rawBytes uint64 = 100_000

	require.False(t, m.routeRequest(&keymapWriteRequest{keys: keys, uncompressedPutBytes: rawBytes}))
	require.Equal(t, rawBytes, m.pendingPutBytes,
		"pendingPutBytes must track the request's raw value bytes, not Address.ValueSize()")
}
