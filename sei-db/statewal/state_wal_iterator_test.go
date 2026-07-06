package statewal

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestIteratorEmpty(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	it, err := w.Iterator(0)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIteratorFromMiddle(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{3, 4, 5}, collectBlocks(t, w, 3))
}

func TestIteratorAcrossFiles(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1 // one block per file
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{2, 3, 4, 5}, collectBlocks(t, w, 2))
}

func TestIteratorWithTinyPrefetchBuffer(t *testing.T) {
	// A prefetch buffer smaller than the number of blocks exercises reader backpressure: the reader must
	// block on a full channel and resume as the consumer drains, without deadlocking or dropping blocks.
	cfg := testConfig(t.TempDir())
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 20; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		collectBlocks(t, w, 1))
}

func TestIteratorCloseBeforeDrainDoesNotLeak(t *testing.T) {
	// Closing an iterator before consuming it must unblock and shut down the reader goroutine cleanly.
	cfg := testConfig(t.TempDir())
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 20; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	// Consume just one block, then close while the reader is still mid-stream (blocked on the full buffer).
	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, it.Close())
	require.NoError(t, it.Close()) // idempotent
}

func TestIteratorReaderExitsWhenWALTornDownWhileOrphaned(t *testing.T) {
	// Regression: an iterator whose reader fills the prefetch buffer and is never consumed or Closed — as
	// happens when Iterator() is aborted via ctx.Done() and the constructed iterator is returned to no one —
	// must not leave its reader goroutine blocked on send() forever. Watching the WAL context lets the reader
	// exit when the WAL is torn down, so it cannot become a zombie.
	cfg := testConfig(t.TempDir())
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 20; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	iter := it.(*walIterator)

	// Do not consume the iterator: the reader fills the prefetch buffer (size 1) and blocks on send. Tear
	// down the WAL context out from under it, as fail() or Close() would.
	w.(*stateWALImpl).cancel()

	select {
	case <-iter.readerExited:
	case <-time.After(5 * time.Second):
		t.Fatal("reader goroutine did not exit after the WAL context was cancelled")
	}

	// A consumer that races the teardown drains whatever the reader had already buffered, then observes an
	// error rather than a clean EOF: a truncated iteration must never masquerade as fully consumed.
	var termErr error
	for i := 0; i < 25; i++ {
		ok, err := it.Next()
		if err != nil {
			termErr = err
			break
		}
		if !ok {
			break
		}
	}
	require.Error(t, termErr, "truncated iteration must surface an error, not a clean EOF")
}

func TestIteratorStopsBeforeIncompleteTail(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	// Block 4 written but not ended.
	require.NoError(t, w.Write(4, []*proto.NamedChangeSet{makeChangeSet("evm", []byte{4}, []byte{4})}))
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{1, 2, 3}, collectBlocks(t, w, 1))
}

func TestIteratorYieldsChangesetContents(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte("key"), []byte("value"))}
	require.NoError(t, w.Write(1, cs))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	entry := it.Entry()
	require.Equal(t, uint64(1), entry.BlockNumber)
	require.False(t, entry.EndOfBlock)
	require.Len(t, entry.Changeset, 1)
	require.Equal(t, "evm", entry.Changeset[0].Name)
	require.Equal(t, []byte("key"), entry.Changeset[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("value"), entry.Changeset[0].Changeset.Pairs[0].Value)

	// The end-of-block marker is folded into the block's single entry, not surfaced separately.
	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

// TestConcurrentIterationDuringRotation hammers the writer with rotate-on-every-block churn while several
// iterators read concurrently. Each iterator seals the mutable file and snapshots its file set at creation on
// the writer goroutine, so every file it reads is sealed and immutable and can never be renamed or rewritten
// out from under an in-flight read; every iteration must be error-free and gap-free.
func TestConcurrentIterationDuringRotation(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1 // rotate (rename) after every block, maximizing the seal/rename churn
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	const totalBlocks = 300
	const readers = 4
	const iterationsPerReader = 40

	var wg sync.WaitGroup

	writeErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for block := uint64(1); block <= totalBlocks; block++ {
			cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte{byte(block)}, []byte{byte(block)})}
			if err := w.Write(block, cs); err != nil {
				writeErr <- err
				return
			}
			if err := w.SignalEndOfBlock(); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	readerErr := make(chan error, readers)
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterationsPerReader; i++ {
				if err := drainContiguousFrom(w, 1); err != nil {
					readerErr <- err
					return
				}
			}
			readerErr <- nil
		}()
	}

	wg.Wait()
	require.NoError(t, <-writeErr)
	for r := 0; r < readers; r++ {
		require.NoError(t, <-readerErr)
	}
}

// drainContiguousFrom fully consumes an iterator anchored at start, verifying the yielded blocks form a
// gap-free, strictly-increasing run beginning at start (an empty run is allowed: the writer may not have
// produced start yet). Returns the first error encountered.
func drainContiguousFrom(w StateWAL, start uint64) error {
	it, err := w.Iterator(start)
	if err != nil {
		return fmt.Errorf("create iterator: %w", err)
	}
	prev := start - 1
	for {
		ok, err := it.Next()
		if err != nil {
			_ = it.Close()
			return fmt.Errorf("next: %w", err)
		}
		if !ok {
			break
		}
		b := it.Entry().BlockNumber
		if b != prev+1 {
			_ = it.Close()
			return fmt.Errorf("non-contiguous iteration: got block %d after %d (start %d)", b, prev, start)
		}
		prev = b
	}
	return it.Close()
}

// TestIteratorDoesNotSeePostConstructionBlocks pins down the snapshot contract: an iterator yields only
// blocks that were complete when it was created, never blocks written afterward. The setup makes the check
// deterministic (no timing race): one block per file plus a prefetch of 1 means the reader blocks on the full
// results channel after the first block and cannot reach later files until the consumer drains, which happens
// only after block 4 is written. Because Iterator() now seals the mutable file at creation, block 4 lands in a
// fresh file outside the snapshot and must not appear.
func TestIteratorDoesNotSeePostConstructionBlocks(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	// Written after the iterator exists, before draining: must not be observed.
	require.NoError(t, w.Write(4, []*proto.NamedChangeSet{makeChangeSet("evm", []byte{4}, []byte{4})}))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	var got []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		got = append(got, it.Entry().BlockNumber)
	}
	require.Equal(t, []uint64{1, 2, 3}, got, "post-construction block 4 must not be iterated")
}

// TestIteratorSealPreservesInProgressBlock verifies the correctness subtlety of sealing at iterator creation:
// when a block is only partially written (several Writes, no end-of-block marker yet), sealing must capture the
// completed blocks for the snapshot AND carry the in-progress block forward without dropping any changeset
// already accepted by Write.
func TestIteratorSealPreservesInProgressBlock(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir())) // large target: everything begins in one mutable file
	defer func() { require.NoError(t, w.Close()) }()

	// Block 1 complete.
	require.NoError(t, w.Write(1, []*proto.NamedChangeSet{makeChangeSet("a", []byte("k1"), []byte("v1"))}))
	require.NoError(t, w.SignalEndOfBlock())
	// Block 2 partially written: one changeset, no end-of-block marker yet.
	require.NoError(t, w.Write(2, []*proto.NamedChangeSet{makeChangeSet("b", []byte("k2"), []byte("v2"))}))

	// Opening the iterator seals block 1 and carries block 2's in-progress record into a fresh mutable file.
	// The incomplete block 2 must not be yielded.
	require.Equal(t, []uint64{1}, collectBlocks(t, w, 1))

	// Finish block 2 with a second changeset, then end it.
	require.NoError(t, w.Write(2, []*proto.NamedChangeSet{makeChangeSet("c", []byte("k3"), []byte("v3"))}))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	// Block 2 must contain BOTH changesets, in write order — nothing lost to the mid-block seal.
	it, err := w.Iterator(2)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	entry := it.Entry()
	require.Equal(t, uint64(2), entry.BlockNumber)
	require.Len(t, entry.Changeset, 2)
	require.Equal(t, "b", entry.Changeset[0].Name)
	require.Equal(t, "c", entry.Changeset[1].Name)

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIteratorCoalescesMultipleWritesInOrder(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	require.NoError(t, w.Write(1, []*proto.NamedChangeSet{makeChangeSet("a", []byte("k1"), []byte("v1"))}))
	require.NoError(t, w.Write(1, []*proto.NamedChangeSet{
		makeChangeSet("b", []byte("k2"), []byte("v2")),
		makeChangeSet("c", []byte("k3"), []byte("v3")),
	}))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)

	entry := it.Entry()
	require.Equal(t, uint64(1), entry.BlockNumber)
	require.False(t, entry.EndOfBlock)
	// Three changesets total (1 from the first Write, 2 from the second), concatenated in write order.
	require.Len(t, entry.Changeset, 3)
	require.Equal(t, "a", entry.Changeset[0].Name)
	require.Equal(t, "b", entry.Changeset[1].Name)
	require.Equal(t, "c", entry.Changeset[2].Name)

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}
