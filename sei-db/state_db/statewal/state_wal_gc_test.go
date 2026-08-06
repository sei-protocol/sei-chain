package statewal

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// openWALForGC opens a WAL and returns it as the collector sees it, closing it on cleanup.
func openWALForGC(t *testing.T, cfg *Config) (StateWAL, gc.PrunableStore) {
	t.Helper()
	w := openWAL(t, cfg)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	store, ok := w.(gc.PrunableStore)
	require.True(t, ok, "a state WAL must satisfy gc.PrunableStore")
	return w, store
}

// The head is the last block ended by SignalEndOfBlock. A block that has been written but not ended
// is still buffered rather than a record, so counting it would put the collector's head one block
// above what the WAL can replay.
func TestGCLatestBlockCountsOnlyCompletedBlocks(t *testing.T) {
	w, store := openWALForGC(t, testConfig(t.TempDir()))

	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "an empty WAL has no head")

	writeBlock(t, w, 1)
	writeBlock(t, w, 2)
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(2), latest)

	// Block 3 is written but not ended, so it is not yet in the WAL.
	require.NoError(t, w.Write(3, []*proto.NamedChangeSet{makeChangeSet("evm", []byte{3}, []byte{3})}))
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(2), latest, "a block in progress must not count as the head")

	require.NoError(t, w.SignalEndOfBlock())
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(3), latest)
}

// The head survives a reopen: a WAL that reported 0 after a restart would drop out of the
// collector's head minimum and let the other stores prune past blocks it still holds.
func TestGCLatestBlockRecoveredOnOpen(t *testing.T) {
	cfg := testConfig(t.TempDir())

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	_, store := openWALForGC(t, cfg)
	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(5), latest)
}

// The WAL asks for no history of its own, and there is no configuration that changes that. What
// holds it back is SC/SS answering their oldest live snapshot as a boundary, which the collector
// applies to every store as the shared minimum — so its depth tracks its consumers automatically
// (see GetRetentionWindow).
func TestGCRetentionWindowIsZero(t *testing.T) {
	_, store := openWALForGC(t, testConfig(t.TempDir()))
	require.Equal(t, int64(0), store.GetRetentionWindow())
}

// A contiguous store answers the cut line it was given whatever it holds, including on an empty WAL
// and above its own head.
func TestGCPruningBoundaryIsAlwaysCutLine(t *testing.T) {
	w, store := openWALForGC(t, testConfig(t.TempDir()))

	require.Equal(t, uint64(42), store.GetPruningBoundary(42))

	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.Equal(t, uint64(3), store.GetPruningBoundary(3))
	require.Equal(t, uint64(1_000), store.GetPruningBoundary(1_000))
}

// PruneBelow goes straight to the WAL, so reclamation does not wait on write traffic. A WAL that has
// stopped receiving blocks is exactly where a deferred prune would strand history indefinitely, so no
// further block is written here before the result is checked.
func TestGCPruneBelowDoesNotWaitForTheNextBlock(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1 // seal after every block, so whole-file pruning can act per block
	w, store := openWALForGC(t, cfg)

	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.NoError(t, store.PruneBelow(6))
	require.NoError(t, w.Flush()) // the prune is async; order behind it

	ok, first, last, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(6), first)
	require.Equal(t, uint64(10), last)

	require.Equal(t, []uint64{6, 7, 8, 9, 10}, collectBlocks(t, w, 6, 10))
}

// Cycles are independent, so a floor lower than one already applied must not walk back the prune it
// performed. Nothing here enforces that — a prune only ever deletes — but the collector is free to
// issue a lower floor after a rollback, and this pins that doing so is harmless rather than a way to
// resurrect blocks or corrupt the range.
func TestGCPruneBelowIgnoresALowerFloor(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1
	w, store := openWALForGC(t, cfg)

	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.NoError(t, store.PruneBelow(8))
	require.NoError(t, store.PruneBelow(3))
	require.NoError(t, w.Flush())

	ok, first, _, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(8), first, "a later, lower floor must not undo the higher one")
}

// The collector runs on its own goroutine while the WAL's owner writes blocks on another, which is
// the whole reason this surface is separate from the writer-facing one. Only the race detector can
// judge it: stateWALImpl keeps its state in plain fields on the assumption of a single caller, and
// what makes the prune safe to issue from here is seiwal.WAL's concurrency carve-out for PruneBefore.
func TestGCConcurrentWithWriter(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1
	w, store := openWALForGC(t, cfg)

	const blocks = 60
	done := make(chan struct{})
	go func() {
		defer close(done)
		for block := uint64(1); block <= blocks; block++ {
			// Assertions are not allowed off the main test goroutine; fail loudly instead.
			cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte{byte(block)}, []byte{byte(block)})}
			if err := w.Write(block, cs); err != nil {
				panic(err)
			}
			if err := w.SignalEndOfBlock(); err != nil {
				panic(err)
			}
		}
	}()

	// Drive prune cycles exactly as the collector does, off the writer's goroutine.
	for range blocks {
		head, err := store.GetLatestBlock()
		require.NoError(t, err)
		require.LessOrEqual(t, head, uint64(blocks))
		if head > 20 {
			require.NoError(t, store.PruneBelow(store.GetPruningBoundary(head-20)))
		}
	}
	<-done

	ok, first, last, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(blocks), last)
	require.LessOrEqual(t, first, uint64(blocks-20), "pruning must never outrun the floor it was given")
}

// A prune issued before a close is ordered ahead of it rather than dropped, and survives into the next
// session. What must not happen is the close failing, or the WAL bricking, because of it.
func TestGCPruneBelowBeforeClose(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.(gc.PrunableStore).PruneBelow(4))
	require.NoError(t, w.Close())

	w2, store := openWALForGC(t, cfg)
	ok, first, last, err := w2.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(4), first)
	require.Equal(t, uint64(5), last)

	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(5), latest)
}

// The collector can still be mid-cycle when the node shuts down, so a prune arriving after the close
// must be reported rather than panic, and must not brick the WAL on the way out — PruneBelow declines
// to write fatalErr precisely because the writer reads it unsynchronized.
func TestGCPruneBelowAfterClose(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	store, ok := w.(gc.PrunableStore)
	require.True(t, ok)
	require.NoError(t, w.Close())

	require.Error(t, store.PruneBelow(4))

	// The head is still readable, and the close committed what was written.
	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(5), latest)

	w2, _ := openWALForGC(t, cfg)
	_, first, last, err := w2.GetStoredRange()
	require.NoError(t, err)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)
}
