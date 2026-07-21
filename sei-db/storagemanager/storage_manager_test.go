package storagemanager

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockSnapshotStore is a hand-written SnapshotStore for tests. It records the last PruneBelow argument and returns
// canned GetStoredBlocks data or errors.
type mockSnapshotStore struct {
	name   string
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

func (m *mockSnapshotStore) Name() string {
	return m.name
}

// mockStreamStore is a hand-written StreamStore for tests.
type mockStreamStore struct {
	name    string
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

func (m *mockStreamStore) Name() string {
	return m.name
}

func toStores(s []*mockSnapshotStore) []SnapshotStore {
	result := make([]SnapshotStore, len(s))
	for i, store := range s {
		result[i] = store
	}
	return result
}

// TestPruneDecisions covers the full decision matrix: which floor each store is pruned to, what the WAL is pruned to,
// and when a cycle is skipped entirely. want == nil means "PruneBelow must not be called".
func TestPruneDecisions(t *testing.T) {
	cases := []struct {
		name           string
		rollbackWindow uint64
		wal            *mockStreamStore
		storeBlocks    [][]uint64
		wantStores     []*uint64 // expected prunedBelow per store; nil => not pruned
		wantWAL        *uint64   // expected WAL prunedBelow; nil => not pruned
	}{
		{
			name:           "single store (flatKV) normal",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{80_000, 85_000, 92_000}},
			wantStores:     []*uint64{ptr(85_000)},
			wantWAL:        ptr(85_000),
		},
		{
			name:           "three stores, WAL floor is the min",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{80_000, 85_000, 92_000}, {70_000, 88_000, 95_000}, {50_000, 90_000, 99_000}},
			wantStores:     []*uint64{ptr(85_000), ptr(88_000), ptr(90_000)},
			wantWAL:        ptr(85_000),
		},
		{
			name:           "rollback window zero => oldestNeeded == latest",
			rollbackWindow: 0,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{80_000, 100_000}, {90_000, 100_000}},
			wantStores:     []*uint64{ptr(100_000), ptr(100_000)},
			wantWAL:        ptr(100_000),
		},
		{
			name:           "latest == rollback window => oldestNeeded == 0",
			rollbackWindow: 100_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{500, 1_000}},
			// oldestNeeded 0, no snapshot <= 0, so floor is the lowest snapshot.
			wantStores: []*uint64{ptr(500)},
			wantWAL:    ptr(500),
		},
		{
			name:           "latest below rollback window => underflow clamped to 0",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 5_000, hasData: true},
			storeBlocks:    [][]uint64{{1_000, 2_000}, {1_500}},
			wantStores:     []*uint64{ptr(1_000), ptr(1_500)},
			wantWAL:        ptr(1_000),
		},
		{
			name:           "snapshot exactly at oldestNeeded is retained",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{50_000, 90_000}, {90_000}},
			wantStores:     []*uint64{ptr(90_000), ptr(90_000)},
			wantWAL:        ptr(90_000),
		},
		{
			name:           "all snapshots newer than oldestNeeded => floor is oldest snapshot",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			// oldestNeeded 90,000; every snapshot is above it.
			storeBlocks: [][]uint64{{95_000, 97_000}, {92_000, 99_000}},
			wantStores:  []*uint64{ptr(95_000), ptr(92_000)},
			wantWAL:     ptr(92_000),
		},
		{
			name:           "mixed: one store at/below oldest, one all-above => WAL is the min",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{85_000, 92_000}, {95_000, 99_000}},
			wantStores:     []*uint64{ptr(85_000), ptr(95_000)},
			wantWAL:        ptr(85_000),
		},
		{
			name:           "no stores + WAL has data => prune WAL to the rollback window",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    nil,
			wantStores:     nil,
			wantWAL:        ptr(90_000),
		},
		{
			name:           "no stores + empty WAL => skip",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{hasData: false},
			storeBlocks:    nil,
			wantStores:     nil,
			wantWAL:        nil,
		},
		{
			name:           "empty WAL => skip everything",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{hasData: false},
			storeBlocks:    [][]uint64{{80_000}, {80_000}},
			wantStores:     []*uint64{nil, nil},
			wantWAL:        nil,
		},
		{
			name:           "one store empty among many => skip everything",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{{80_000}, nil, {80_000}},
			wantStores:     []*uint64{nil, nil, nil},
			wantWAL:        nil,
		},
		{
			name:           "all stores empty => skip everything",
			rollbackWindow: 10_000,
			wal:            &mockStreamStore{start: 1, end: 100_000, hasData: true},
			storeBlocks:    [][]uint64{nil, nil},
			wantStores:     []*uint64{nil, nil},
			wantWAL:        nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mocks := make([]*mockSnapshotStore, len(tc.storeBlocks))
			for i, blocks := range tc.storeBlocks {
				mocks[i] = &mockSnapshotStore{name: fmt.Sprintf("store%d", i), blocks: blocks}
			}
			tc.wal.name = "stateWAL"

			require.NoError(t, prune(tc.rollbackWindow, toStores(mocks), tc.wal))

			for i, want := range tc.wantStores {
				if want == nil {
					require.Falsef(t, mocks[i].pruneBelowCalled, "store%d should not be pruned", i)
				} else {
					require.Truef(t, mocks[i].pruneBelowCalled, "store%d should be pruned", i)
					require.Equalf(t, *want, mocks[i].prunedBelow, "store%d prune floor", i)
				}
			}
			if tc.wantWAL == nil {
				require.False(t, tc.wal.pruneBelowCalled, "WAL should not be pruned")
			} else {
				require.True(t, tc.wal.pruneBelowCalled, "WAL should be pruned")
				require.Equal(t, *tc.wantWAL, tc.wal.prunedBelow, "WAL prune floor")
			}
		})
	}
}

func TestPruneWALGetError(t *testing.T) {
	sentinel := errors.New("boom")
	a := &mockSnapshotStore{name: "a", blocks: []uint64{80_000}}
	wal := &mockStreamStore{name: "stateWAL", getErr: sentinel}

	err := prune(10_000, toStores([]*mockSnapshotStore{a}), wal)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "stateWAL")
	require.False(t, a.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneStoreGetError(t *testing.T) {
	sentinel := errors.New("boom")
	a := &mockSnapshotStore{name: "commitStore", getErr: sentinel}
	wal := &mockStreamStore{name: "stateWAL", start: 1, end: 100_000, hasData: true}

	err := prune(10_000, toStores([]*mockSnapshotStore{a}), wal)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "commitStore")
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneStorePruneErrorStopsBeforeLaterStoresAndWAL(t *testing.T) {
	sentinel := errors.New("boom")
	// The first store fails to prune; later stores and the WAL must be left untouched.
	a := &mockSnapshotStore{name: "a", blocks: []uint64{80_000}, pruneErr: sentinel}
	b := &mockSnapshotStore{name: "b", blocks: []uint64{80_000}}
	wal := &mockStreamStore{name: "stateWAL", start: 1, end: 100_000, hasData: true}

	err := prune(10_000, toStores([]*mockSnapshotStore{a, b}), wal)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "a")
	require.True(t, a.pruneBelowCalled)
	require.False(t, b.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestPruneWALPruneError(t *testing.T) {
	sentinel := errors.New("boom")
	// All stores prune successfully, then the WAL prune fails.
	a := &mockSnapshotStore{name: "a", blocks: []uint64{80_000}}
	b := &mockSnapshotStore{name: "b", blocks: []uint64{85_000}}
	wal := &mockStreamStore{name: "stateWAL", start: 1, end: 100_000, hasData: true, pruneErr: sentinel}

	err := prune(10_000, toStores([]*mockSnapshotStore{a, b}), wal)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "stateWAL")
	require.ErrorContains(t, err, "80000") // WAL floor = min(store floors)
	require.True(t, a.pruneBelowCalled)
	require.True(t, b.pruneBelowCalled)
	require.True(t, wal.pruneBelowCalled)
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
		{name: "target zero all above", blocks: []uint64{5, 10}, target: 0, wantFloor: 5},
		{name: "duplicate blocks", blocks: []uint64{50, 50, 100, 100}, target: 100, wantFloor: 100},
		{name: "brackets target", blocks: []uint64{99, 101}, target: 100, wantFloor: 99},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantFloor, snapshotPruningFloor(tc.blocks, tc.target))
		})
	}
}

// TestSnapshotPruningFloorProperty checks snapshotPruningFloor against a brute-force oracle over random sorted input:
// the result must be the highest block <= target, or the lowest block when none qualify.
func TestSnapshotPruningFloorProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		n := 1 + rng.Intn(8)
		blocks := make([]uint64, n)
		var next uint64
		for i := range blocks {
			next += uint64(rng.Intn(5)) // ascending, allows duplicates
			blocks[i] = next
		}
		target := uint64(rng.Intn(int(next) + 5))

		// Oracle: highest block <= target, or the lowest block (blocks[0]) when none qualify.
		want := blocks[0]
		for _, b := range blocks {
			if b <= target {
				want = b
			}
		}

		require.Equalf(t, want, snapshotPruningFloor(blocks, target), "blocks=%v target=%d", blocks, target)
	}
}

func TestBlocksAtOrAbove(t *testing.T) {
	require.Empty(t, blocksAtOrAbove(nil, 100))
	require.Equal(t, []uint64{100, 150}, blocksAtOrAbove([]uint64{50, 100, 150}, 100))
	require.Equal(t, []uint64{10, 20, 30}, blocksAtOrAbove([]uint64{10, 20, 30}, 0))
	require.Empty(t, blocksAtOrAbove([]uint64{10, 20}, 100))
	// Floor strictly between elements.
	require.Equal(t, []uint64{60, 70}, blocksAtOrAbove([]uint64{50, 60, 70}, 55))
}

// TestBlocksAtOrAboveProperty checks blocksAtOrAbove equals the filter b >= floor, preserving order.
func TestBlocksAtOrAboveProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for iter := 0; iter < 2000; iter++ {
		n := rng.Intn(8)
		blocks := make([]uint64, n)
		for i := range blocks {
			blocks[i] = uint64(rng.Intn(50))
		}
		floor := uint64(rng.Intn(50))

		want := []uint64{}
		for _, b := range blocks {
			if b >= floor {
				want = append(want, b)
			}
		}
		got := blocksAtOrAbove(blocks, floor)
		require.Equalf(t, want, got, "blocks=%v floor=%d", blocks, floor)
	}
}

func TestBlockRange(t *testing.T) {
	require.Equal(t, "[1, 100]", blockRange(1, 100))
	require.Equal(t, "[0, 0]", blockRange(0, 0))
}

func TestDefaultStorageManagerConfig(t *testing.T) {
	cfg := DefaultStorageManagerConfig()
	require.Equal(t, uint64(10_000), cfg.RollbackWindow)
	require.Equal(t, uint64(60), cfg.PruneIntervalSeconds)
}

func TestValidate(t *testing.T) {
	require.NoError(t, DefaultStorageManagerConfig().Validate())

	// A zero rollback window is legal.
	require.NoError(t, (&StorageManagerConfig{RollbackWindow: 0, PruneIntervalSeconds: 60}).Validate())

	err := (&StorageManagerConfig{PruneIntervalSeconds: 0}).Validate()
	require.ErrorContains(t, err, "prune interval")

	// The largest interval that does not overflow time.Duration is accepted; one larger is rejected.
	maxInterval := uint64(math.MaxInt64) / uint64(time.Second)
	require.NoError(t, (&StorageManagerConfig{PruneIntervalSeconds: maxInterval}).Validate())
	require.ErrorContains(t, (&StorageManagerConfig{PruneIntervalSeconds: maxInterval + 1}).Validate(), "at most")
}

func TestNewStorageManagerInvalidConfig(t *testing.T) {
	sm, err := NewStorageManager(
		context.Background(),
		&StorageManagerConfig{PruneIntervalSeconds: 0},
		toStores([]*mockSnapshotStore{{name: "a"}}),
		&mockStreamStore{name: "stateWAL"},
	)
	require.Error(t, err)
	require.Nil(t, sm)
}

func TestNewStorageManagerConstructAndClose(t *testing.T) {
	a := &mockSnapshotStore{name: "a", blocks: []uint64{100}}
	wal := &mockStreamStore{name: "stateWAL", start: 1, end: 100, hasData: true}
	sm, err := NewStorageManager(
		context.Background(),
		&StorageManagerConfig{RollbackWindow: 10, PruneIntervalSeconds: 60},
		toStores([]*mockSnapshotStore{a}),
		wal,
	)
	require.NoError(t, err)
	require.NotNil(t, sm)

	// Close must return without hanging.
	require.NoError(t, sm.Close())
}

func ptr(v uint64) *uint64 {
	return &v
}
