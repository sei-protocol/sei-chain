package statewal

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A delivery a hashRecorder received.
type recordedHash struct {
	blockNumber uint64
	hash        [32]byte
}

// hashRecorder is a HashListener that records every delivery.
type hashRecorder struct {
	mu         sync.Mutex
	deliveries []recordedHash
}

func (r *hashRecorder) listen(_ context.Context, blockHeight uint64, hash [32]byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries = append(r.deliveries, recordedHash{blockNumber: blockHeight, hash: hash})
	return nil
}

func (r *hashRecorder) blocks() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	blocks := make([]uint64, 0, len(r.deliveries))
	for _, d := range r.deliveries {
		blocks = append(blocks, d.blockNumber)
	}
	return blocks
}

// iteratedHashes returns the hash GetHash() reports for each block in [start, end].
func iteratedHashes(t *testing.T, w StateWAL, start uint64, end uint64) []recordedHash {
	t.Helper()
	it, err := w.Iterator(start, end)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var hashes []recordedHash
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			return hashes
		}
		blockNumber, _ := it.Entry()
		hash, err := it.GetHash()
		require.NoError(t, err)
		hashes = append(hashes, recordedHash{blockNumber: blockNumber, hash: hash})
	}
}

// Every block written reaches the listener once, in order, with the hash the iterator reports for it.
func TestHashListenerReceivesEveryBlock(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	recorder := &hashRecorder{}
	ok, _, _, err := w.RegisterHashListener(recorder.listen)
	require.NoError(t, err)
	require.False(t, ok, "an empty WAL has no hash yet")

	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, iteratedHashes(t, w, 1, 10), recorder.deliveries)
}

// Distinct blocks get distinct hashes, and an empty block still gets one.
func TestHashDependsOnChangesets(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	writeBlock(t, w, 1)
	writeBlock(t, w, 2)
	require.NoError(t, w.Write(3, nil))
	require.NoError(t, w.Flush())

	hashes := iteratedHashes(t, w, 1, 3)
	require.NotEqual(t, hashes[0].hash, hashes[1].hash)
	require.NotEqual(t, hashes[1].hash, hashes[2].hash)
}

// A listener registered mid-stream is told the most recent hash delivered, and its first delivery is the block
// after it.
func TestHashListenerRegisteredMidStream(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	recorder := &hashRecorder{}
	ok, blockNumber, hash, err := w.RegisterHashListener(recorder.listen)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), blockNumber)
	require.Equal(t, iteratedHashes(t, w, 5, 5)[0].hash, hash)

	writeBlock(t, w, 6)
	require.NoError(t, w.Flush())
	require.Equal(t, []uint64{6}, recorder.blocks())
}

// A reopened WAL reports the hash of its last stored block until it delivers another.
func TestHashSeededFromLastStoredBlockOnReopen(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	want := iteratedHashes(t, w, 3, 3)[0].hash
	require.NoError(t, w.Close())

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, blockNumber, hash, err := w2.RegisterHashListener(nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(3), blockNumber)
	require.Equal(t, want, hash)
}

// A listener error bricks the WAL, and no listener receives a hash for any later block.
func TestHashListenerErrorBricksWAL(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	recorder := &hashRecorder{}
	_, _, _, err := w.RegisterHashListener(func(ctx context.Context, blockHeight uint64, hash [32]byte) error {
		if blockHeight == 2 {
			return errInjected
		}
		return recorder.listen(ctx, blockHeight, hash)
	})
	require.NoError(t, err)

	writeBlock(t, w, 1)
	writeBlock(t, w, 2)
	require.ErrorIs(t, w.Flush(), errInjected)

	requireBricked(t, w)
	require.Equal(t, []uint64{1}, recorder.blocks())
}

// Flush returns only once every listener has returned for every block written before it.
func TestFlushWaitsForHashListeners(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	recorder := &hashRecorder{}
	_, _, _, err := w.RegisterHashListener(func(ctx context.Context, blockHeight uint64, hash [32]byte) error {
		time.Sleep(time.Millisecond)
		return recorder.listen(ctx, blockHeight, hash)
	})
	require.NoError(t, err)

	for block := uint64(1); block <= 20; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.Len(t, recorder.blocks(), 20)
}

// Once the request channel is full behind a blocked listener, Write blocks until the listener catches up.
func TestWriteBlocksWhileRequestBufferIsFull(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.RequestBufferSize = 0
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	_, _, _, err := w.RegisterHashListener(func(ctx context.Context, _ uint64, _ [32]byte) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	require.NoError(t, err)

	writeBlock(t, w, 1) // taken by the writer, which blocks in the listener
	<-entered

	written := make(chan error, 1)
	go func() {
		written <- w.Write(2, nil)
	}()
	select {
	case err := <-written:
		close(release)
		t.Fatalf("Write returned while the request buffer was full: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-written)
	require.NoError(t, w.Flush())
}
