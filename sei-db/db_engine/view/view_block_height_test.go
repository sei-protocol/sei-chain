package view

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
)

// newTestManagerAtHeight builds a manager over db opened at height, closed when the test ends.
func newTestManagerAtHeight(t *testing.T, db *testDB, height uint64) ViewManager {
	t.Helper()
	pool := threading.NewAdHocPool()
	manager, err := NewViewManager(newTestConfig(1, 4096), height, db, pool, pool, pool)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = manager.Close()
		pool.Close()
		_ = db.Close()
	})
	return manager
}

// The first block a manager seals is the height it was opened at, including height 0, and its views flush.
func TestFirstCommitSealsTheHeightTheManagerWasOpenedAt(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerAtHeight(t, db, 0)

	_, err := manager.Commit(1)
	require.Error(t, err)

	baseline, err := manager.Commit(0)
	require.NoError(t, err)
	require.Equal(t, uint64(0), baseline.BlockHeight())
	finalizeAwaitFlushAndRelease(t, baseline)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	block, err := manager.Commit(1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), block.BlockHeight())
	finalizeAwaitFlushAndRelease(t, block)

	value, err := db.Get([]byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("v"), value)
}

func TestCommitRejectsAnyHeightButTheNextBlock(t *testing.T) {
	manager := newTestManagerAtHeight(t, newTestDB(nil), 7)

	block7, err := manager.Commit(7)
	require.NoError(t, err)
	finalizeAndRelease(t, block7)

	for _, height := range []uint64{6, 7, 9} {
		_, err := manager.Commit(height)
		require.Error(t, err, "sealing block %d after block 7", height)
	}

	block8, err := manager.Commit(8)
	require.NoError(t, err)
	finalizeAndRelease(t, block8)
}

func TestFinalizeRejectsAnotherBlocksHeight(t *testing.T) {
	manager := newTestManagerAtHeight(t, newTestDB(nil), 7)
	view, err := manager.Commit(7)
	require.NoError(t, err)

	require.Error(t, view.Finalize(8, nil))
	require.NoError(t, view.Finalize(7, nil))
	require.NoError(t, view.Release())
}

func TestLatestViewIsTheLastSealedView(t *testing.T) {
	manager := newTestManagerAtHeight(t, newTestDB(nil), 7)
	_, err := manager.LatestView()
	require.Error(t, err)

	block7, err := manager.Commit(7)
	require.NoError(t, err)
	latest, err := manager.LatestView()
	require.NoError(t, err)
	require.Equal(t, block7, latest)

	finalizeAndRelease(t, block7)
	require.NoError(t, latest.Release())
}
