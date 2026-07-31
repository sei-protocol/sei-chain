package gc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockStore is a PrunableStore with canned answers. Build via snapshotStore or contiguousStore.
type mockStore struct {
	name         string
	latestHeight uint64
	// retentionWindow is returned by GetRetentionWindow (0 = shared RollbackWindow only).
	retentionWindow int64
	pruningBoundry  func(cutLine uint64) uint64
	getErr          error
	pruneErr        error

	pruneBelowCalled atomic.Bool
	prunedBelow      atomic.Uint64
}

// snapshotStore models SC/SS (retention 0). GetPruningBoundry returns the newest snapshot
// ≤ cutLine, or the oldest snapshot if all are above cutLine. Empty snapshots → 0.
// Snapshots must be ascending.
func snapshotStore(name string, latestHeight uint64, snapshots ...uint64) *mockStore {
	return &mockStore{
		name:            name,
		latestHeight:    latestHeight,
		retentionWindow: 0,
		pruningBoundry: func(cutLine uint64) uint64 {
			if len(snapshots) == 0 {
				return 0
			}
			oldest := snapshots[0]
			for _, snapshot := range snapshots {
				if snapshot > cutLine {
					break
				}
				oldest = snapshot
			}
			return oldest
		},
	}
}

// contiguousStore models blockDB / receiptDB / WAL (retention 0 by default).
// hasData=false → GetPruningBoundry returns 0 (opt out). Otherwise returns cutLine.
// Use withRetentionWindow for extras or InfiniteRetentionWindow.
func contiguousStore(name string, latestHeight uint64, hasData bool) *mockStore {
	return &mockStore{
		name:            name,
		latestHeight:    latestHeight,
		retentionWindow: 0,
		pruningBoundry: func(cutLine uint64) uint64 {
			if !hasData {
				return 0
			}
			return cutLine
		},
	}
}

func withRetentionWindow(store *mockStore, retention int64) *mockStore {
	store.retentionWindow = retention
	return store
}

func (m *mockStore) Name() string {
	return m.name
}

func (m *mockStore) GetRetentionWindow() int64 {
	return m.retentionWindow
}

func (m *mockStore) GetLastestBlock() (uint64, error) {
	return m.latestHeight, m.getErr
}

func (m *mockStore) GetPruningBoundry(cutLine uint64) uint64 {
	return m.pruningBoundry(cutLine)
}

func (m *mockStore) PruneBelow(blockNumber uint64) error {
	m.pruneBelowCalled.Store(true)
	m.prunedBelow.Store(blockNumber)
	return m.pruneErr
}

func prunableStores(list ...*mockStore) []PrunableStore {
	result := make([]PrunableStore, len(list))
	for i, store := range list {
		result[i] = store
	}
	return result
}

func testConfig(t *testing.T, rollbackWindow uint64) *StorageGarbageCollectorConfig {
	t.Helper()
	config := &StorageGarbageCollectorConfig{
		RollbackWindow: rollbackWindow,
		PruneInterval:  time.Minute,
	}
	require.NoError(t, config.Validate())
	return config
}

// TestPruneDecisions covers one prune cycle. wantPruneBelow == nil means no store is pruned.
func TestPruneDecisions(t *testing.T) {
	cases := []struct {
		name           string
		rollbackWindow uint64
		stores         []*mockStore
		wantPruneBelow *uint64
	}{
		{
			name:           "SC and WAL both retention 0: min boundry wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// cutLine 90_000; sc keeps snapshot 85_000; WAL answers 90_000 → min 85_000.
			wantPruneBelow: ptr(85_000),
		},
		{
			name:           "lowest boundry across many stores wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				snapshotStore("ss", 100_000, 70_000, 88_000, 95_000),
				snapshotStore("flatKV", 100_000, 50_000, 90_000, 99_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// sc 85_000, ss 88_000, flatKV 90_000, WAL 90_000 → min 85_000.
			wantPruneBelow: ptr(85_000),
		},
		{
			name:           "RollbackWindow of 1 still leaves one block of margin",
			rollbackWindow: 1,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 99_999),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(99_999),
		},
		{
			name:           "positive contiguous retention deepens that store's cut line",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 90_000),
				withRetentionWindow(contiguousStore("stateWAL", 100_000, true), 5_000),
			},
			// sc cutLine 90_000 → 90_000; WAL cutLine 85_000 → 85_000 → min 85_000.
			wantPruneBelow: ptr(85_000),
		},
		{
			name:           "SS retention 0 behaves like SC",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("ss", 100_000, 80_000, 90_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(90_000),
		},
		{
			name:           "infinite retention on every store skips pruning",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				withRetentionWindow(contiguousStore("blockDB", 100_000, true), InfiniteRetentionWindow),
				withRetentionWindow(contiguousStore("receiptDB", 100_000, true), InfiniteRetentionWindow),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "infinite retention on one store leaves others free to prune",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				withRetentionWindow(contiguousStore("archiveWAL", 100_000, true), InfiniteRetentionWindow),
				snapshotStore("sc", 100_000, 80_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// archiveWAL skipped; sc 80_000; stateWAL 90_000 → min 80_000.
			wantPruneBelow: ptr(80_000),
		},
		{
			name:           "snapshot exactly at the cut line is kept",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 50_000, 90_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(90_000),
		},
		{
			name:           "all snapshots above cut line: store still votes its oldest",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000, 97_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// sc answers 95_000; WAL answers 90_000 → shared prune 90_000 (sc snapshots untouched).
			wantPruneBelow: ptr(90_000),
		},
		{
			name:           "lagging store lowers the global head",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("lagging", 50_000, 30_000, 50_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// head 50_000 → cutLine 40_000; lagging answers 30_000; WAL 40_000 → min 30_000.
			wantPruneBelow: ptr(30_000),
		},
		{
			name:           "store answering 0 is ignored",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				contiguousStore("optOut", 100_000, false),
				snapshotStore("sc", 100_000, 80_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(80_000),
		},
		{
			name:           "store with no snapshot yet is ignored",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(90_000),
		},
		{
			name:           "zero head ignored for global head; store still votes",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("stalled", 0, 50_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// head from WAL 100_000; stalled still answers 50_000 → min 50_000.
			wantPruneBelow: ptr(50_000),
		},
		{
			name:           "head inside retain window: no prune",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 5_000, 1_000, 2_000),
				contiguousStore("stateWAL", 5_000, true),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "head inside one store's window skips only that store",
			rollbackWindow: 60_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 500, 1_000),
				withRetentionWindow(contiguousStore("stateWAL", 100_000, true), 40_000),
			},
			// WAL cutLine 0 (skipped); sc cutLine 40_000 → 1_000.
			wantPruneBelow: ptr(1_000),
		},
		{
			name:           "no store has a latest block",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 0),
				contiguousStore("stateWAL", 0, false),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "only an opt-out store has data",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				contiguousStore("optOut", 100_000, false),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "no stores at all",
			rollbackWindow: 10_000,
			stores:         nil,
			wantPruneBelow: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stores := prunableStores(tc.stores...)
			require.NoError(t, prune(testConfig(t, tc.rollbackWindow), stores))

			head, err := getGlobalLastestBlock(stores)
			require.NoError(t, err)

			for _, store := range tc.stores {
				shouldPrune := false
				if tc.wantPruneBelow != nil && store.GetRetentionWindow() >= 0 {
					cutLine := getCutLine(head, tc.rollbackWindow, store.GetRetentionWindow())
					if cutLine > 0 && store.GetPruningBoundry(cutLine) != 0 {
						shouldPrune = true
					}
				}
				if !shouldPrune {
					require.Falsef(t, store.pruneBelowCalled.Load(), "%s should not be pruned", store.name)
					continue
				}
				require.Truef(t, store.pruneBelowCalled.Load(), "%s should be pruned", store.name)
				require.Equalf(t, *tc.wantPruneBelow, store.prunedBelow.Load(), "%s prune height", store.name)
			}
		})
	}
}

func TestPruneOnYoungChainWithDeepContiguousRetention(t *testing.T) {
	// head 100 << WAL retain window (rollback 1_000 + retention 100_000) → both cutLines 0.
	sc := snapshotStore("sc", 100, 50, 100)
	wal := withRetentionWindow(contiguousStore("stateWAL", 100, true), 100_000)

	require.NoError(t, prune(testConfig(t, 1_000), prunableStores(sc, wal)))

	require.False(t, sc.pruneBelowCalled.Load())
	require.False(t, wal.pruneBelowCalled.Load())
}

func TestPruneKeepsOptedOutStoreWithDuplicateName(t *testing.T) {
	// Answers are keyed by index, so a duplicate Name() must not prune the opt-out store.
	optOut := contiguousStore("ss", 100_000, false)
	participating := snapshotStore("ss", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000, true)

	require.NoError(t, prune(testConfig(t, 10_000), prunableStores(optOut, participating, wal)))

	require.False(t, optOut.pruneBelowCalled.Load(), "store answering 0 must not be pruned")
	require.True(t, participating.pruneBelowCalled.Load())
	require.Equal(t, uint64(80_000), participating.prunedBelow.Load())
	require.Equal(t, uint64(80_000), wal.prunedBelow.Load())
}

func TestPruneGetLastestBlockError(t *testing.T) {
	sentinel := errors.New("boom")
	sc := snapshotStore("sc", 100_000, 80_000)
	broken := contiguousStore("brokenStore", 100_000, true)
	broken.getErr = sentinel

	err := prune(testConfig(t, 10_000), prunableStores(sc, broken))
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "brokenStore")
	require.False(t, sc.pruneBelowCalled.Load())
	require.False(t, broken.pruneBelowCalled.Load())
}

func TestPruneBelowErrorStopsRemainingStores(t *testing.T) {
	sentinel := errors.New("boom")
	first := snapshotStore("first", 100_000, 80_000)
	first.pruneErr = sentinel
	second := snapshotStore("second", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000, true)

	err := prune(testConfig(t, 10_000), prunableStores(first, second, wal))
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "first")
	require.ErrorContains(t, err, "80000")
	require.True(t, first.pruneBelowCalled.Load())
	require.False(t, second.pruneBelowCalled.Load())
	require.False(t, wal.pruneBelowCalled.Load())
}

func TestGetCutLine(t *testing.T) {
	cases := []struct {
		name           string
		head           uint64
		rollbackWindow uint64
		retention      int64
		want           uint64
	}{
		{name: "rollback window only", head: 100_000, rollbackWindow: 10_000, want: 90_000},
		{name: "retention adds to rollback window", head: 100_000, rollbackWindow: 1, retention: 10_000, want: 89_999},
		{name: "the two windows add", head: 100_000, rollbackWindow: 10_000, retention: 5_000, want: 85_000},
		{name: "zero retention", head: 100_000, rollbackWindow: 10_000, want: 90_000},
		{name: "infinite retention", head: 100_000, rollbackWindow: 10_000, retention: InfiniteRetentionWindow, want: 0},
		{name: "any negative retention is infinite", head: 100_000, rollbackWindow: 10_000, retention: -99, want: 0},
		{name: "head one above the window", head: 10_001, rollbackWindow: 10_000, want: 1},
		{name: "head exactly at the window", head: 10_000, rollbackWindow: 10_000, want: 0},
		{name: "head one below the window", head: 9_999, rollbackWindow: 10_000, want: 0},
		{name: "head far below the window", head: 100, rollbackWindow: 1_000, retention: 100_000, want: 0},
		{name: "head at genesis", head: 0, rollbackWindow: 10_000, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, getCutLine(tc.head, tc.rollbackWindow, tc.retention))
		})
	}
}

func TestGetGlobalLastestBlock(t *testing.T) {
	storesWithHeads := func(heads ...uint64) []PrunableStore {
		list := make([]*mockStore, len(heads))
		for i, head := range heads {
			list[i] = contiguousStore("store", head, true)
		}
		return prunableStores(list...)
	}

	cases := []struct {
		name  string
		heads []uint64
		want  uint64
	}{
		{name: "single store", heads: []uint64{100}, want: 100},
		{name: "all agree", heads: []uint64{100, 100, 100}, want: 100},
		{name: "smallest first", heads: []uint64{80, 100}, want: 80},
		{name: "smallest last", heads: []uint64{100, 80}, want: 80},
		{name: "smallest in the middle", heads: []uint64{100, 50, 90}, want: 50},
		{name: "leading zero ignored", heads: []uint64{0, 100}, want: 100},
		{name: "trailing zero ignored", heads: []uint64{100, 0}, want: 100},
		{name: "zero among many ignored", heads: []uint64{100, 0, 80}, want: 80},
		{name: "every store reports zero", heads: []uint64{0, 0}, want: 0},
		{name: "no stores", heads: nil, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, err := getGlobalLastestBlock(storesWithHeads(tc.heads...))
			require.NoError(t, err)
			require.Equal(t, tc.want, head)
		})
	}
}

func TestDefaultStorageGarbageCollectorConfig(t *testing.T) {
	cfg := DefaultStorageGarbageCollectorConfig()
	require.Equal(t, uint64(1_000), cfg.RollbackWindow)
	require.Equal(t, 10*time.Minute, cfg.PruneInterval)
	require.NoError(t, cfg.Validate())
}

func TestValidate(t *testing.T) {
	require.ErrorContains(t, (*StorageGarbageCollectorConfig)(nil).Validate(), "config is required")

	require.ErrorContains(t, (&StorageGarbageCollectorConfig{
		RollbackWindow: 0,
		PruneInterval:  time.Minute,
	}).Validate(), "rollback window must be greater than 0")

	require.NoError(t, (&StorageGarbageCollectorConfig{
		RollbackWindow: 1,
		PruneInterval:  time.Minute,
	}).Validate())

	require.ErrorContains(t, (&StorageGarbageCollectorConfig{
		RollbackWindow: 1,
		PruneInterval:  0,
	}).Validate(), "prune interval")
	require.ErrorContains(t, (&StorageGarbageCollectorConfig{
		RollbackWindow: 1,
		PruneInterval:  -time.Second,
	}).Validate(), "prune interval")
}

func TestNewStorageGarbageCollectorInvalidConfig(t *testing.T) {
	sm, err := NewStorageGarbageCollector(
		context.Background(),
		&StorageGarbageCollectorConfig{PruneInterval: 0},
		prunableStores(snapshotStore("sc", 100)),
	)
	require.Error(t, err)
	require.Nil(t, sm)
}

func TestNewStorageGarbageCollectorNilConfig(t *testing.T) {
	sm, err := NewStorageGarbageCollector(context.Background(), nil, nil)
	require.ErrorContains(t, err, "config is required")
	require.Nil(t, sm)
}

func TestNewStorageGarbageCollectorConstructAndClose(t *testing.T) {
	sm, err := NewStorageGarbageCollector(
		context.Background(),
		DefaultStorageGarbageCollectorConfig(),
		prunableStores(snapshotStore("sc", 100, 100), contiguousStore("stateWAL", 100, true)),
	)
	require.NoError(t, err)
	require.NotNil(t, sm)

	require.NoError(t, sm.Close())
}

func TestCloseAfterContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sm, err := NewStorageGarbageCollector(ctx, DefaultStorageGarbageCollectorConfig(), nil)
	require.NoError(t, err)

	cancel()
	require.NoError(t, sm.Close())
}

func ptr(v uint64) *uint64 {
	return &v
}
