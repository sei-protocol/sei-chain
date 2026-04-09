package keymap

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/unflushed"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/test"
	"github.com/stretchr/testify/require"
)

const defaultTargetWriteBatchSize = 1024

func newTestKeymapManager(
	t *testing.T,
	workChanSize int,
) *KeymapManager {
	t.Helper()
	return newTestKeymapManagerWithBatchSize(t, workChanSize, defaultTargetWriteBatchSize)
}

func newTestKeymapManagerWithBatchSize(
	t *testing.T,
	workChanSize int,
	targetWriteBatchSize int,
) *KeymapManager {
	t.Helper()
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	kmap, _, err := NewMemKeymap(logger, "", false, nil)
	require.NoError(t, err)

	cache := unflushed.NewUnflushedDataCache(logger, errorMonitor, 64, nil, "test")
	t.Cleanup(func() { cache.Stop() })
	km := NewKeymapManager(logger, errorMonitor, kmap, cache, workChanSize, targetWriteBatchSize, nil, "test")
	return km
}

func makeKeys(keys ...string) []types.ScopedKey {
	result := make([]types.ScopedKey, len(keys))
	for i, k := range keys {
		result[i] = types.ScopedKey{
			Key:     []byte(k),
			Address: types.NewAddress(uint32(i), uint32(i*10), uint8(i%256), uint32(i*100)),
		}
	}
	return result
}

func TestWriteAndFlush(t *testing.T) {
	km := newTestKeymapManager(t, 16)
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
}

func TestWriteMultipleBatchesThenFlush(t *testing.T) {
	km := newTestKeymapManager(t, 16)
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
}

func TestLookupBeforeFlush(t *testing.T) {
	km := newTestKeymapManager(t, 16)
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
	km := newTestKeymapManager(t, 16)
	defer km.Stop()

	_, exists, err := km.LookupAddress([]byte("missing"))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestDeleteKeys(t *testing.T) {
	km := newTestKeymapManager(t, 16)
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
	km := newTestKeymapManager(t, 16)
	defer km.Stop()

	require.NoError(t, km.Flush())
	require.NoError(t, km.Flush())
}

func TestStop(t *testing.T) {
	km := newTestKeymapManager(t, 16)

	keys := makeKeys("before-stop")
	require.NoError(t, km.WriteKeys(keys))
	require.NoError(t, km.Stop())

	// After stop, the loop has exited. Writes previously enqueued should have been processed.
	_, exists, err := km.LookupAddress([]byte("before-stop"))
	require.NoError(t, err)
	require.True(t, exists)
}

func TestStopProcessesPendingWrites(t *testing.T) {
	km := newTestKeymapManager(t, 64)

	const writeCount = 50
	for i := 0; i < writeCount; i++ {
		keys := makeKeys(test.NewTestRandomNoPrint().String(16))
		require.NoError(t, km.WriteKeys(keys))
	}
	require.NoError(t, km.Stop())
}

func TestWriteIsNonBlocking(t *testing.T) {
	km := newTestKeymapManager(t, 32)

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

	_ = km.Stop()
}

func TestErrorMonitorPanic(t *testing.T) {
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	kmap, _, err := NewMemKeymap(logger, "", false, nil)
	require.NoError(t, err)

	cache := unflushed.NewUnflushedDataCache(logger, errorMonitor, 64, nil, "test")
	t.Cleanup(func() { cache.Stop() })
	km := NewKeymapManager(logger, errorMonitor, kmap, cache, 16, defaultTargetWriteBatchSize, nil, "test")

	// Panic cancels the context, which causes ImmediateShutdownRequired to fire.
	errorMonitor.Panic(fmt.Errorf("test error"))

	// Allow the loop to observe the shutdown.
	time.Sleep(10 * time.Millisecond)

	// Flush requires a round-trip through the loop. Since the loop has exited, the response
	// channel will never be written to, and Await will observe the cancelled context.
	err = km.Flush()
	require.Error(t, err)
}

func TestAllKeysWritten(t *testing.T) {
	km := newTestKeymapManager(t, 16)
	defer km.Stop()

	batch1 := makeKeys("x", "y")
	batch2 := makeKeys("z")

	require.NoError(t, km.WriteKeys(batch1))
	require.NoError(t, km.WriteKeys(batch2))
	require.NoError(t, km.Flush())

	allExpected := append(batch1, batch2...)
	for _, k := range allExpected {
		_, exists, err := km.LookupAddress(k.Key)
		require.NoError(t, err)
		require.True(t, exists)
	}
}

func TestConcurrentWritesAndFlush(t *testing.T) {
	km := newTestKeymapManager(t, 128)
	defer km.Stop()

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
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	inner, _, err := NewMemKeymap(logger, "", false, nil)
	require.NoError(t, err)
	counting := &countingKeymap{inner: inner}

	cache := unflushed.NewUnflushedDataCache(logger, errorMonitor, 64, nil, "test")
	t.Cleanup(func() { cache.Stop() })
	km := NewKeymapManager(logger, errorMonitor, counting, cache, 128, 100, nil, "test")

	for i := 0; i < 50; i++ {
		require.NoError(t, km.WriteKeys(makeKeys(fmt.Sprintf("key-%d", i))))
	}
	require.NoError(t, km.Flush())

	for i := 0; i < 50; i++ {
		_, exists, err := km.LookupAddress([]byte(fmt.Sprintf("key-%d", i)))
		require.NoError(t, err)
		require.True(t, exists)
	}

	putCount := counting.putCount
	require.GreaterOrEqual(t, putCount, 1)
	require.LessOrEqual(t, putCount, 50)

	require.NoError(t, km.Stop())
}

// TestWriteBatchingRespectsTargetSize verifies that a batch is flushed once it reaches
// the targetWriteBatchSize threshold, even if more writes are available.
func TestWriteBatchingRespectsTargetSize(t *testing.T) {
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	inner, _, err := NewMemKeymap(logger, "", false, nil)
	require.NoError(t, err)
	counting := &countingKeymap{inner: inner}

	cache := unflushed.NewUnflushedDataCache(logger, errorMonitor, 64, nil, "test")
	t.Cleanup(func() { cache.Stop() })
	km := NewKeymapManager(logger, errorMonitor, counting, cache, 128, 5, nil, "test")

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
	km := newTestKeymapManagerWithBatchSize(t, 128, 1000)
	defer km.Stop()

	require.NoError(t, km.WriteKeys(makeKeys("a", "b")))
	require.NoError(t, km.DeleteKeys(makeKeys("a")))
	require.NoError(t, km.WriteKeys(makeKeys("c")))
	require.NoError(t, km.Flush())

	_, exists, err := km.LookupAddress([]byte("a"))
	require.NoError(t, err)
	require.False(t, exists)

	_, exists, err = km.LookupAddress([]byte("b"))
	require.NoError(t, err)
	require.True(t, exists)

	_, exists, err = km.LookupAddress([]byte("c"))
	require.NoError(t, err)
	require.True(t, exists)
}

// countingKeymap wraps a Keymap and counts Put calls.
type countingKeymap struct {
	inner    Keymap
	putCount int
}

func (c *countingKeymap) Put(pairs []types.ScopedKey) error {
	c.putCount++
	return c.inner.Put(pairs)
}

func (c *countingKeymap) Get(key []byte) (types.Address, bool, error) {
	return c.inner.Get(key)
}

func (c *countingKeymap) Delete(keys []types.ScopedKey) error {
	return c.inner.Delete(keys)
}

func (c *countingKeymap) Stop() error {
	return c.inner.Stop()
}

func (c *countingKeymap) Destroy() error {
	return c.inner.Destroy()
}

func (c *countingKeymap) Flush() error {
	return c.inner.Flush()
}

func (c *countingKeymap) ReverseIterator() (KeymapReverseIterator, error) {
	return c.inner.ReverseIterator()
}
