package view

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
)

// newTestManagerAtHeight builds a manager that starts at initialHeight and flushes behind commitCounter.
func newTestManagerAtHeight(
	t *testing.T,
	initialHeight uint64,
	commitCounter threading.ReadOnlyCounter,
) (ViewManager, *testDB) {
	t.Helper()
	db := newTestDB(nil)
	pool := threading.NewAdHocPool()
	manager, err := NewViewManager(newTestConfig(1, 1<<20), initialHeight, commitCounter, db, pool, pool, pool)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = manager.Close()
		pool.Close()
		_ = db.Close()
	})
	return manager, db
}

// commitAndFinalize writes key=value, seals the version and finalizes it at its height. The caller keeps the
// reservation Commit returned.
func commitAndFinalize(t *testing.T, manager ViewManager, key string, value string) View {
	t.Helper()
	require.NoError(t, manager.Set([]byte(key), []byte(value)))
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(heightOf(view), nil))
	return view
}

func TestVersionsStartAtInitialHeight(t *testing.T) {
	for _, initialHeight := range []uint64{0, 1, 1000} {
		manager, _ := newTestManagerAtHeight(t, initialHeight, threading.NoOpCounter{})
		for i := uint64(0); i < 3; i++ {
			view, err := manager.Commit()
			require.NoError(t, err)
			require.Equal(t, initialHeight+i, heightOf(view))
			require.NoError(t, view.Finalize(initialHeight+i, nil))
			awaitFlushed(t, view, time.Second)
			require.NoError(t, view.Release())
		}
	}
}

func TestFinalizeRejectsWrongHeight(t *testing.T) {
	manager, _ := newTestManagerAtHeight(t, 10, threading.NoOpCounter{})
	view, err := manager.Commit()
	require.NoError(t, err)
	require.Error(t, view.Finalize(11, nil))
	require.Error(t, view.Finalize(9, nil))
	require.NoError(t, view.Finalize(10, nil))
	require.NoError(t, view.Release())
}

func TestNewViewManagerRejectsNilCommitCounter(t *testing.T) {
	pool := threading.NewAdHocPool()
	t.Cleanup(pool.Close)
	_, err := NewViewManager(newTestConfig(1, 1<<20), 0, nil, newTestDB(nil), pool, pool, pool)
	require.Error(t, err)
}

func TestFlushWaitsForCommitCounter(t *testing.T) {
	counter := threading.NewCounter(4)
	manager, db := newTestManagerAtHeight(t, 5, counter)

	first := commitAndFinalize(t, manager, "a", "1")
	second := commitAndFinalize(t, manager, "b", "2")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.Error(t, first.AwaitFlush(ctx), "block 5 must not flush before the commit counter reaches it")
	require.False(t, db.has("a"))

	_, err := counter.IncrementN(2)
	require.NoError(t, err)
	awaitFlushed(t, first, time.Second)
	require.True(t, db.has("a"))
	require.NoError(t, first.Release())
	awaitFlushed(t, second, time.Second)
	require.True(t, db.has("b"))
	require.NoError(t, second.Release())
}

func TestFlushAbandonedWhenCommitCounterSealedBelow(t *testing.T) {
	counter := threading.NewCounter(4)
	manager, db := newTestManagerAtHeight(t, 5, counter)

	view := commitAndFinalize(t, manager, "a", "1")
	require.NoError(t, counter.Seal())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.Error(t, view.AwaitFlush(ctx), "a flush behind a sealed commit counter must be abandoned")
	require.False(t, db.has("a"))
	require.Empty(t, db.committedBatches())
	require.NoError(t, view.Release())

	// Abandoning the flush does not brick the manager.
	_, _, err := manager.Get([]byte("a"), false)
	require.NoError(t, err)
	require.NoError(t, manager.Close())
}
