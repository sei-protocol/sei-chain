package statewal

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A delivery a budRecorder received.
type recordedBUD struct {
	blockNumber uint64
	bud         [32]byte
}

// budRecorder is a BUDListener that records every delivery.
type budRecorder struct {
	mu         sync.Mutex
	deliveries []recordedBUD
}

func (r *budRecorder) listen(_ context.Context, blockHeight uint64, bud [32]byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries = append(r.deliveries, recordedBUD{blockNumber: blockHeight, bud: bud})
	return nil
}

func (r *budRecorder) blocks() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	blocks := make([]uint64, 0, len(r.deliveries))
	for _, d := range r.deliveries {
		blocks = append(blocks, d.blockNumber)
	}
	return blocks
}

// iteratedBUDs returns the BUD GetHash() reports for each block in [start, end].
func iteratedBUDs(t *testing.T, w StateWAL, start uint64, end uint64) []recordedBUD {
	t.Helper()
	it, err := w.Iterator(start, end)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var buds []recordedBUD
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			return buds
		}
		blockNumber, _ := it.Entry()
		bud, err := it.GetHash()
		require.NoError(t, err)
		buds = append(buds, recordedBUD{blockNumber: blockNumber, bud: bud})
	}
}

// Every block written reaches the listener once, in order, with the BUD the iterator reports for it.
func TestBUDListenerReceivesEveryBlock(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	recorder := &budRecorder{}
	ok, _, _, err := w.RegisterBUDListener(recorder.listen)
	require.NoError(t, err)
	require.False(t, ok, "an empty WAL has no BUD yet")

	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, iteratedBUDs(t, w, 1, 10), recorder.deliveries)
}

// Distinct blocks get distinct BUDs, and an empty block still gets one.
func TestBUDDependsOnChangesets(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	writeBlock(t, w, 1)
	writeBlock(t, w, 2)
	require.NoError(t, w.Write(3, nil))
	require.NoError(t, w.Flush())

	buds := iteratedBUDs(t, w, 1, 3)
	require.NotEqual(t, buds[0].bud, buds[1].bud)
	require.NotEqual(t, buds[1].bud, buds[2].bud)
}

// A listener registered mid-stream is told the most recent BUD delivered, and its first delivery is the block
// after it.
func TestBUDListenerRegisteredMidStream(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	recorder := &budRecorder{}
	ok, blockNumber, bud, err := w.RegisterBUDListener(recorder.listen)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), blockNumber)
	require.Equal(t, iteratedBUDs(t, w, 5, 5)[0].bud, bud)

	writeBlock(t, w, 6)
	require.NoError(t, w.Flush())
	require.Equal(t, []uint64{6}, recorder.blocks())
}

// A reopened WAL reports the BUD of its last stored block until it delivers another.
func TestBUDSeededFromLastStoredBlockOnReopen(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	want := iteratedBUDs(t, w, 3, 3)[0].bud
	require.NoError(t, w.Close())

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, blockNumber, bud, err := w2.RegisterBUDListener(nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(3), blockNumber)
	require.Equal(t, want, bud)
}

// A listener error bricks the WAL, and no listener receives a BUD for any later block.
func TestBUDListenerErrorBricksWAL(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	recorder := &budRecorder{}
	_, _, _, err := w.RegisterBUDListener(func(ctx context.Context, blockHeight uint64, bud [32]byte) error {
		if blockHeight == 2 {
			return errInjected
		}
		return recorder.listen(ctx, blockHeight, bud)
	})
	require.NoError(t, err)

	writeBlock(t, w, 1)
	writeBlock(t, w, 2)
	require.ErrorIs(t, w.Flush(), errInjected)

	requireBricked(t, w)
	require.Equal(t, []uint64{1}, recorder.blocks())
}

// Flush returns only once every listener has returned for every block written before it.
func TestFlushWaitsForBUDListeners(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	recorder := &budRecorder{}
	_, _, _, err := w.RegisterBUDListener(func(ctx context.Context, blockHeight uint64, bud [32]byte) error {
		time.Sleep(time.Millisecond)
		return recorder.listen(ctx, blockHeight, bud)
	})
	require.NoError(t, err)

	for block := uint64(1); block <= 20; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.Len(t, recorder.blocks(), 20)
}

// Close returns while a listener is blocked, because the listener's ctx is cancelled.
func TestCloseCancelsBlockedBUDListener(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))

	entered := make(chan struct{})
	_, _, _, err := w.RegisterBUDListener(func(ctx context.Context, _ uint64, _ [32]byte) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	require.NoError(t, err)

	writeBlock(t, w, 1)
	<-entered
	require.NoError(t, w.Close())
}

// A full BUD channel blocks Write until the listener catches up.
func TestWriteBlocksWhileBUDBufferIsFull(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.BUDBufferSize = 1
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	_, _, _, err := w.RegisterBUDListener(func(ctx context.Context, _ uint64, _ [32]byte) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	require.NoError(t, err)

	writeBlock(t, w, 1) // taken by the BUD goroutine, which blocks in the listener
	<-entered
	writeBlock(t, w, 2) // fills the buffer

	written := make(chan error, 1)
	go func() {
		written <- w.Write(3, nil)
	}()
	select {
	case err := <-written:
		t.Fatalf("Write returned while the BUD buffer was full: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-written)
	require.NoError(t, w.Flush())
}
