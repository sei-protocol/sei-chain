package storagemanager

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockSnapshotStore is a hand-written SnapshotStore for tests. It records the last PruneBelow argument and returns
// canned GetStoredBlocks data or errors.
type mockSnapshotStore struct {
	blocks []uint64
	getErr error

	pruneBelowCalled bool
	prunedBelow      uint64
	pruneErr         error
}

func (m *mockSnapshotStore) GetStoredBlocks() ([]uint64, error) {
	return m.blocks, m.getErr
}

func (m *mockSnapshotStore) PruneBelow(blockNumber uint64) error {
	m.pruneBelowCalled = true
	m.prunedBelow = blockNumber
	return m.pruneErr
}

// mockStreamStore is a hand-written StreamStore for tests.
type mockStreamStore struct {
	start   uint64
	end     uint64
	hasData bool
	getErr  error

	pruneBelowCalled bool
	prunedBelow      uint64
	pruneErr         error
}

func (m *mockStreamStore) GetStoredBlocks() (uint64, uint64, bool, error) {
	return m.start, m.end, m.hasData, m.getErr
}

func (m *mockStreamStore) PruneBelow(blockNumber uint64) error {
	m.pruneBelowCalled = true
	m.prunedBelow = blockNumber
	return m.pruneErr
}

func TestPruneNormal(t *testing.T) {
	// latest 100,000 with a 10,000 rollback window => oldestNeeded 90,000.
	commit := &mockSnapshotStore{blocks: []uint64{80_000, 85_000, 92_000}}
	state := &mockSnapshotStore{blocks: []uint64{70_000, 88_000, 95_000}}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	require.NoError(t, prune(10_000, commit, state, wal))

	// Highest snapshot <= 90,000 for each store.
	require.True(t, commit.pruneBelowCalled)
	require.Equal(t, uint64(85_000), commit.prunedBelow)
	require.True(t, state.pruneBelowCalled)
	require.Equal(t, uint64(88_000), state.prunedBelow)

	// WAL retains from the minimum of the two floors.
	require.True(t, wal.pruneBelowCalled)
	require.Equal(t, uint64(85_000), wal.prunedBelow)
}

func TestPruneUnderflowGuard(t *testing.T) {
	// latest below the rollback window => oldestNeeded clamped to 0.
	commit := &mockSnapshotStore{blocks: []uint64{1_000, 2_000}}
	state := &mockSnapshotStore{blocks: []uint64{1_500}}
	wal := &mockStreamStore{start: 1, end: 5_000, hasData: true}

	require.NoError(t, prune(10_000, commit, state, wal))

	// No snapshot is <= 0, so each floor is the lowest stored block (nothing is actually removed).
	require.Equal(t, uint64(1_000), commit.prunedBelow)
	require.Equal(t, uint64(1_500), state.prunedBelow)
	require.Equal(t, uint64(1_000), wal.prunedBelow)
}

func TestPruneExactBoundaryRetained(t *testing.T) {
	// A snapshot exactly at oldestNeeded must be kept (floor == oldestNeeded).
	commit := &mockSnapshotStore{blocks: []uint64{50_000, 90_000}}
	state := &mockSnapshotStore{blocks: []uint64{90_000}}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	require.NoError(t, prune(10_000, commit, state, wal))

	require.Equal(t, uint64(90_000), commit.prunedBelow)
	require.Equal(t, uint64(90_000), state.prunedBelow)
	require.Equal(t, uint64(90_000), wal.prunedBelow)
}

func TestPruneEmptyWALSkipsAll(t *testing.T) {
	commit := &mockSnapshotStore{blocks: []uint64{80_000}}
	state := &mockSnapshotStore{blocks: []uint64{80_000}}
	wal := &mockStreamStore{hasData: false}

	require.NoError(t, prune(10_000, commit, state, wal))

	// Nothing committed yet: no store should be pruned.
	require.False(t, commit.pruneBelowCalled)
	require.False(t, state.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneEmptyCommitStoreSkipsAll(t *testing.T) {
	// An empty snapshot store is treated as unknown (a snapshot may be mid-write), so pruning is skipped entirely to
	// avoid breaking the rollback invariant.
	commit := &mockSnapshotStore{blocks: nil}
	state := &mockSnapshotStore{blocks: []uint64{80_000}}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	require.NoError(t, prune(10_000, commit, state, wal))

	require.False(t, commit.pruneBelowCalled)
	require.False(t, state.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneEmptyStateStoreSkipsAll(t *testing.T) {
	// Mirror of the above: an empty state store also disables all pruning.
	commit := &mockSnapshotStore{blocks: []uint64{80_000}}
	state := &mockSnapshotStore{blocks: nil}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	require.NoError(t, prune(10_000, commit, state, wal))

	require.False(t, commit.pruneBelowCalled)
	require.False(t, state.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneWALGetError(t *testing.T) {
	commit := &mockSnapshotStore{blocks: []uint64{80_000}}
	state := &mockSnapshotStore{blocks: []uint64{80_000}}
	wal := &mockStreamStore{getErr: errors.New("boom")}

	err := prune(10_000, commit, state, wal)
	require.ErrorContains(t, err, "state WAL")
	require.False(t, commit.pruneBelowCalled)
	require.False(t, state.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneCommitGetError(t *testing.T) {
	commit := &mockSnapshotStore{getErr: errors.New("boom")}
	state := &mockSnapshotStore{blocks: []uint64{80_000}}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	err := prune(10_000, commit, state, wal)
	require.ErrorContains(t, err, "commit store")
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneStateGetError(t *testing.T) {
	commit := &mockSnapshotStore{blocks: []uint64{80_000}}
	state := &mockSnapshotStore{getErr: errors.New("boom")}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	err := prune(10_000, commit, state, wal)
	require.ErrorContains(t, err, "state store")
	require.False(t, wal.pruneBelowCalled)
}

func TestPrunePruneErrorPropagates(t *testing.T) {
	commit := &mockSnapshotStore{blocks: []uint64{80_000}, pruneErr: errors.New("boom")}
	state := &mockSnapshotStore{blocks: []uint64{80_000}}
	wal := &mockStreamStore{start: 1, end: 100_000, hasData: true}

	err := prune(10_000, commit, state, wal)
	require.ErrorContains(t, err, "commit store")
}

func TestSnapshotPruningFloor(t *testing.T) {
	cases := []struct {
		name      string
		blocks    []uint64
		target    uint64
		wantFloor uint64
	}{
		{name: "single below", blocks: []uint64{50}, target: 100, wantFloor: 50},
		{name: "single above", blocks: []uint64{150}, target: 100, wantFloor: 150},
		{name: "single exact", blocks: []uint64{100}, target: 100, wantFloor: 100},
		{name: "all below picks max", blocks: []uint64{10, 20, 30}, target: 100, wantFloor: 30},
		{name: "all above picks min", blocks: []uint64{150, 200, 300}, target: 100, wantFloor: 150},
		{name: "exact match preferred", blocks: []uint64{50, 100, 150}, target: 100, wantFloor: 100},
		{name: "mixed", blocks: []uint64{10, 40, 90, 150, 200}, target: 100, wantFloor: 90},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantFloor, snapshotPruningFloor(tc.blocks, tc.target))
		})
	}
}

func TestBlocksAtOrAbove(t *testing.T) {
	require.Empty(t, blocksAtOrAbove(nil, 100))
	require.Equal(t, []uint64{100, 150}, blocksAtOrAbove([]uint64{50, 100, 150}, 100))
	require.Equal(t, []uint64{10, 20, 30}, blocksAtOrAbove([]uint64{10, 20, 30}, 0))
	require.Empty(t, blocksAtOrAbove([]uint64{10, 20}, 100))
}

func TestValidate(t *testing.T) {
	require.NoError(t, DefaultStorageManagerConfig().Validate())

	// A zero rollback window is legal.
	require.NoError(t, (&StorageManagerConfig{RollbackWindow: 0, PruneIntervalSeconds: 60}).Validate())

	require.Error(t, (&StorageManagerConfig{PruneIntervalSeconds: 0}).Validate())
}

func TestNewStorageManagerInvalidConfig(t *testing.T) {
	sm, err := NewStorageManager(
		context.Background(),
		&StorageManagerConfig{PruneIntervalSeconds: 0},
		&mockSnapshotStore{},
		&mockSnapshotStore{},
		&mockStreamStore{},
	)
	require.Error(t, err)
	require.Nil(t, sm)
}

func TestStorageManagerLifecycle(t *testing.T) {
	commit := &mockSnapshotStore{blocks: []uint64{100}}
	state := &mockSnapshotStore{blocks: []uint64{100}}
	wal := &mockStreamStore{start: 1, end: 100, hasData: true}
	sm, err := NewStorageManager(
		context.Background(),
		&StorageManagerConfig{RollbackWindow: 10, PruneIntervalSeconds: 1},
		commit,
		state,
		wal,
	)
	require.NoError(t, err)
	require.NotNil(t, sm)

	// The run loop must exit cleanly when Close is called.
	require.NoError(t, sm.Close())
}
