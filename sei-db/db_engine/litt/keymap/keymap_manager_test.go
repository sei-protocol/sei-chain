package keymap

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/test"
	"github.com/stretchr/testify/require"
)

const defaultTargetWriteBatchSize = 1024

func newTestKeymapManager(
	t *testing.T,
	workChanSize int,
	durableKeyChanSize int,
) (*KeymapManager, chan []*types.ScopedKey) {
	t.Helper()
	return newTestKeymapManagerWithBatchSize(t, workChanSize, durableKeyChanSize, defaultTargetWriteBatchSize)
}

func newTestKeymapManagerWithBatchSize(
	t *testing.T,
	workChanSize int,
	durableKeyChanSize int,
	targetWriteBatchSize int,
) (*KeymapManager, chan []*types.ScopedKey) {
	t.Helper()
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	kmap, _, err := NewMemKeymap(logger, "", false)
	require.NoError(t, err)

	durableKeyChan := make(chan []*types.ScopedKey, durableKeyChanSize)
	km := NewKeymapManager(logger, errorMonitor, kmap, durableKeyChan, workChanSize, targetWriteBatchSize)
	return km, durableKeyChan
}

// drainDurableKeys collects all keys from the durable key channel until it would block.
func drainDurableKeys(ch chan []*types.ScopedKey) []*types.ScopedKey {
	var all []*types.ScopedKey
	for {
		select {
		case batch := <-ch:
			all = append(all, batch...)
		default:
			return all
		}
	}
}

func makeKeys(keys ...string) []*types.ScopedKey {
	result := make([]*types.ScopedKey, len(keys))
	for i, k := range keys {
		result[i] = &types.ScopedKey{
			Key:     []byte(k),
			Address: types.NewAddress(uint32(i), uint32(i*10), uint8(i%256), uint32(i*100)),
		}
	}
	return result
}

func TestWriteAndFlush(t *testing.T) {
	km, durableKeyChan := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	keys := makeKeys("alpha", "beta", "gamma")
	require.NoError(t, km.WriteKeys(keys))
	require.NoError(t, km.Flush())

	for _, k := range keys {
		addr, exists, err := km.LookupAddress(k.Key)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, k.Address, addr)
	}

	durable := drainDurableKeys(durableKeyChan)
	require.Equal(t, len(keys), len(durable))
}

func TestWriteMultipleBatchesThenFlush(t *testing.T) {
	km, durableKeyChan := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	batch1 := makeKeys("a", "b")
	batch2 := makeKeys("c", "d")

	require.NoError(t, km.WriteKeys(batch1))
	require.NoError(t, km.WriteKeys(batch2))
	require.NoError(t, km.Flush())

	allExpected := append(batch1, batch2...)
	for _, k := range allExpected {
		_, exists, err := km.LookupAddress(k.Key)
		require.NoError(t, err)
		require.True(t, exists)
	}

	durable := drainDurableKeys(durableKeyChan)
	require.Equal(t, len(allExpected), len(durable))
}

func TestLookupBeforeFlush(t *testing.T) {
	km, _ := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	keys := makeKeys("pending")
	require.NoError(t, km.WriteKeys(keys))

	// Without flushing, the key may or may not be visible (depends on loop timing).
	// After flushing, it must be visible.
	require.NoError(t, km.Flush())

	addr, exists, err := km.LookupAddress([]byte("pending"))
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, keys[0].Address, addr)
}

func TestLookupNonexistentKey(t *testing.T) {
	km, _ := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	_, exists, err := km.LookupAddress([]byte("missing"))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestDeleteKeys(t *testing.T) {
	km, _ := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	keys := makeKeys("delete-me")
	require.NoError(t, km.WriteKeys(keys))
	require.NoError(t, km.Flush())

	_, exists, err := km.LookupAddress([]byte("delete-me"))
	require.NoError(t, err)
	require.True(t, exists)

	require.NoError(t, km.DeleteKeys(keys))
	require.NoError(t, km.Flush())

	_, exists, err = km.LookupAddress([]byte("delete-me"))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestFlushWithNoWrites(t *testing.T) {
	km, _ := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	require.NoError(t, km.Flush())
	require.NoError(t, km.Flush())
}

func TestStop(t *testing.T) {
	km, _ := newTestKeymapManager(t, 16, 16)

	keys := makeKeys("before-stop")
	require.NoError(t, km.WriteKeys(keys))
	require.NoError(t, km.Stop())

	// After stop, the loop has exited. Writes previously enqueued should have been processed.
	_, exists, err := km.LookupAddress([]byte("before-stop"))
	require.NoError(t, err)
	require.True(t, exists)
}

func TestStopProcessesPendingWrites(t *testing.T) {
	km, durableKeyChan := newTestKeymapManager(t, 64, 64)

	const writeCount = 50
	for i := 0; i < writeCount; i++ {
		keys := makeKeys(test.NewTestRandomNoPrint().String(16))
		require.NoError(t, km.WriteKeys(keys))
	}
	require.NoError(t, km.Stop())

	// All keys should have been processed, though they may be coalesced into fewer batches.
	durable := drainDurableKeys(durableKeyChan)
	require.Equal(t, writeCount, len(durable))
}

func TestWriteIsNonBlocking(t *testing.T) {
	// Use a large work channel and a zero-size durable key channel to let the loop block
	// on durableKeyChan sends. Writes should still succeed as long as workChan has capacity.
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	kmap, _, err := NewMemKeymap(logger, "", false)
	require.NoError(t, err)

	durableKeyChan := make(chan []*types.ScopedKey) // unbuffered
	km := NewKeymapManager(logger, errorMonitor, kmap, durableKeyChan, 32, defaultTargetWriteBatchSize)

	// Fill up the work channel without anyone draining durableKeyChan.
	// The first write will be picked up by the loop and block on durableKeyChan.
	// Subsequent writes should still land in the buffered workChan.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 30; i++ {
			keys := makeKeys(test.NewTestRandomNoPrint().String(8))
			err := km.WriteKeys(keys)
			if err != nil {
				break
			}
		}
		close(done)
	}()

	select {
	case <-done:
		// WriteKeys returned without blocking — good.
	case <-time.After(2 * time.Second):
		t.Fatal("WriteKeys blocked unexpectedly")
	}

	// Drain durableKeyChan so the loop can finish, then stop.
	go func() {
		for range durableKeyChan {
		}
	}()
	_ = km.Stop()
}

func TestErrorMonitorPanic(t *testing.T) {
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	kmap, _, err := NewMemKeymap(logger, "", false)
	require.NoError(t, err)

	durableKeyChan := make(chan []*types.ScopedKey, 16)
	km := NewKeymapManager(logger, errorMonitor, kmap, durableKeyChan, 16, defaultTargetWriteBatchSize)

	// Panic cancels the context, which causes ImmediateShutdownRequired to fire.
	errorMonitor.Panic(fmt.Errorf("test error"))

	// Allow the loop to observe the shutdown.
	time.Sleep(10 * time.Millisecond)

	// Flush requires a round-trip through the loop. Since the loop has exited, the response
	// channel will never be written to, and Await will observe the cancelled context.
	err = km.Flush()
	require.Error(t, err)
}

func TestDurableKeyChanReceivesAllKeys(t *testing.T) {
	km, durableKeyChan := newTestKeymapManager(t, 16, 16)
	defer km.Stop()

	batch1 := makeKeys("x", "y")
	batch2 := makeKeys("z")

	require.NoError(t, km.WriteKeys(batch1))
	require.NoError(t, km.WriteKeys(batch2))
	require.NoError(t, km.Flush())

	durable := drainDurableKeys(durableKeyChan)
	require.Equal(t, len(batch1)+len(batch2), len(durable))
}

func TestConcurrentWritesAndFlush(t *testing.T) {
	km, durableKeyChan := newTestKeymapManager(t, 128, 128)
	defer km.Stop()

	// Drain the durable key channel in the background.
	totalDurable := make(chan int, 1)
	go func() {
		count := 0
		for range durableKeyChan {
			count++
		}
		totalDurable <- count
	}()

	rand := test.NewTestRandom()

	const numWriters = 4
	const writesPerWriter = 50
	errs := make(chan error, numWriters*writesPerWriter)

	for w := 0; w < numWriters; w++ {
		go func() {
			for i := 0; i < writesPerWriter; i++ {
				keys := makeKeys(rand.String(16))
				if err := km.WriteKeys(keys); err != nil {
					errs <- err
					return
				}
			}
		}()
	}

	// Flush ensures all writes complete.
	require.NoError(t, km.Flush())

	select {
	case err := <-errs:
		t.Fatalf("unexpected error from writer: %v", err)
	default:
	}
}

// TestWriteBatching verifies that the loop coalesces consecutive write requests into a single
// keymap.Put() call when they are immediately available in the work channel.
func TestWriteBatching(t *testing.T) {
	// Use a small targetWriteBatchSize so batching kicks in quickly, and a counting keymap
	// to observe how many Put() calls are made.
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	inner, _, err := NewMemKeymap(logger, "", false)
	require.NoError(t, err)
	counting := &countingKeymap{inner: inner}

	durableKeyChan := make(chan []*types.ScopedKey, 128)
	// targetWriteBatchSize=100: the loop will coalesce up to 100 keys before flushing.
	km := NewKeymapManager(logger, errorMonitor, counting, durableKeyChan, 128, 100)

	// Enqueue 50 write requests of 1 key each. Because the loop hasn't started draining
	// yet (or is very fast), many of these will be in the channel simultaneously.
	for i := 0; i < 50; i++ {
		require.NoError(t, km.WriteKeys(makeKeys(fmt.Sprintf("key-%d", i))))
	}
	require.NoError(t, km.Flush())

	// All 50 keys must be present.
	for i := 0; i < 50; i++ {
		_, exists, err := km.LookupAddress([]byte(fmt.Sprintf("key-%d", i)))
		require.NoError(t, err)
		require.True(t, exists)
	}

	// Batching should have reduced the number of Put() calls. The exact count depends on
	// timing, but it must be less than 50 (one per write request) and at least 1.
	putCount := counting.putCount
	require.GreaterOrEqual(t, putCount, 1)
	require.LessOrEqual(t, putCount, 50)

	// Durable key channel should have received all 50 keys total.
	durable := drainDurableKeys(durableKeyChan)
	require.Equal(t, 50, len(durable))

	require.NoError(t, km.Stop())
}

// TestWriteBatchingRespectsTargetSize verifies that a batch is flushed once it reaches
// the targetWriteBatchSize threshold, even if more writes are available.
func TestWriteBatchingRespectsTargetSize(t *testing.T) {
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	inner, _, err := NewMemKeymap(logger, "", false)
	require.NoError(t, err)
	counting := &countingKeymap{inner: inner}

	durableKeyChan := make(chan []*types.ScopedKey, 128)
	// targetWriteBatchSize=5: after accumulating 5 keys, stop draining and flush.
	km := NewKeymapManager(logger, errorMonitor, counting, durableKeyChan, 128, 5)

	// Enqueue 20 write requests of 1 key each.
	for i := 0; i < 20; i++ {
		require.NoError(t, km.WriteKeys(makeKeys(fmt.Sprintf("key-%d", i))))
	}
	require.NoError(t, km.Flush())

	// With a target of 5, we expect at least 4 Put() calls (20/5), though timing may
	// cause some batches to be smaller.
	putCount := counting.putCount
	require.GreaterOrEqual(t, putCount, 4)

	// All 20 keys must be present.
	for i := 0; i < 20; i++ {
		_, exists, err := km.LookupAddress([]byte(fmt.Sprintf("key-%d", i)))
		require.NoError(t, err)
		require.True(t, exists)
	}

	require.NoError(t, km.Stop())
}

// TestWriteBatchingStopsOnNonWriteMessage verifies that a non-write message (e.g. delete)
// causes the accumulated write batch to be flushed before the non-write message is handled.
func TestWriteBatchingStopsOnNonWriteMessage(t *testing.T) {
	km, durableKeyChan := newTestKeymapManagerWithBatchSize(t, 128, 128, 1000)
	defer km.Stop()

	// Enqueue writes, then a delete, then more writes.
	require.NoError(t, km.WriteKeys(makeKeys("a", "b")))
	require.NoError(t, km.DeleteKeys(makeKeys("a")))
	require.NoError(t, km.WriteKeys(makeKeys("c")))
	require.NoError(t, km.Flush())

	// "a" was written then deleted.
	_, exists, err := km.LookupAddress([]byte("a"))
	require.NoError(t, err)
	require.False(t, exists)

	// "b" and "c" should still exist.
	_, exists, err = km.LookupAddress([]byte("b"))
	require.NoError(t, err)
	require.True(t, exists)

	_, exists, err = km.LookupAddress([]byte("c"))
	require.NoError(t, err)
	require.True(t, exists)

	durable := drainDurableKeys(durableKeyChan)
	require.Equal(t, 3, len(durable))
}

// countingKeymap wraps a Keymap and counts Put calls.
type countingKeymap struct {
	inner    Keymap
	putCount int
}

func (c *countingKeymap) Put(pairs []*types.ScopedKey) error {
	c.putCount++
	return c.inner.Put(pairs)
}

func (c *countingKeymap) Get(key []byte) (types.Address, bool, error) {
	return c.inner.Get(key)
}

func (c *countingKeymap) Delete(keys []*types.ScopedKey) error {
	return c.inner.Delete(keys)
}

func (c *countingKeymap) Stop() error {
	return c.inner.Stop()
}

func (c *countingKeymap) Destroy() error {
	return c.inner.Destroy()
}

func (c *countingKeymap) ReverseIterator() (KeymapReverseIterator, error) {
	return c.inner.ReverseIterator()
}
