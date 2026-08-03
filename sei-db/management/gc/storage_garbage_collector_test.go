package gc

import (
	"context"
	"errors"
	"math"
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
	pruningBoundary func(cutLine uint64) uint64
	getErr          error
	pruneErr        error

	pruneBelowCalled atomic.Bool
	prunedBelow      atomic.Uint64
	pruneBelowCalls  atomic.Uint64
	boundaryCalls    atomic.Uint64
}

// snapshotStore models SC/SS (retention 0). GetPruningBoundary returns the newest snapshot
// ≤ cutLine, or cutLine when every snapshot is above it (nothing can be dropped, and the
// contract forbids answering above cutLine). No snapshots at all → CannotServeRollback.
// Snapshots must be ascending.
func snapshotStore(name string, latestHeight uint64, snapshots ...uint64) *mockStore {
	return &mockStore{
		name:            name,
		latestHeight:    latestHeight,
		retentionWindow: 0,
		pruningBoundary: func(cutLine uint64) uint64 {
			if len(snapshots) == 0 {
				return CannotServeRollback
			}
			if snapshots[0] > cutLine {
				return cutLine
			}
			newest := snapshots[0]
			for _, snapshot := range snapshots {
				if snapshot > cutLine {
					break
				}
				newest = snapshot
			}
			return newest
		},
	}
}

// contiguousStore models blockDB / receiptDB / WAL (retention 0 by default): it can restore to
// any height it holds, so it always answers cutLine — including when it holds nothing at or below
// cutLine, where the resulting PruneBelow is simply a no-op. Use withRetentionWindow for extra
// retention or InfiniteRetentionWindow.
func contiguousStore(name string, latestHeight uint64) *mockStore {
	return &mockStore{
		name:            name,
		latestHeight:    latestHeight,
		retentionWindow: 0,
		pruningBoundary: func(cutLine uint64) uint64 { return cutLine },
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

func (m *mockStore) GetLatestBlock() (uint64, error) {
	return m.latestHeight, m.getErr
}

func (m *mockStore) GetPruningBoundary(cutLine uint64) uint64 {
	m.boundaryCalls.Add(1)
	return m.pruningBoundary(cutLine)
}

func (m *mockStore) PruneBelow(blockNumber uint64) error {
	m.pruneBelowCalled.Store(true)
	m.prunedBelow.Store(blockNumber)
	m.pruneBelowCalls.Add(1)
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
//
// wantPruned is hardcoded per store rather than derived from getCutLine / GetPruningBoundary: deriving it from the
// helpers under test would let a sign error or off-by-one shift the expectation in lockstep with the bug.
func TestPruneDecisions(t *testing.T) {
	cases := []struct {
		name           string
		rollbackWindow uint64
		stores         []*mockStore
		wantPruneBelow *uint64
		wantPruned     []bool
	}{
		{
			name:           "SC and WAL both retention 0: min boundary wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				contiguousStore("stateWAL", 100_000),
			},
			// cutLine 90_000; sc keeps snapshot 85_000; WAL answers 90_000 → min 85_000.
			wantPruneBelow: ptr(85_000),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "lowest boundary across many stores wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				snapshotStore("ss", 100_000, 70_000, 88_000, 95_000),
				snapshotStore("flatKV", 100_000, 50_000, 90_000, 99_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc 85_000, ss 88_000, flatKV 90_000, WAL 90_000 → min 85_000.
			wantPruneBelow: ptr(85_000),
			wantPruned:     []bool{true, true, true, true},
		},
		{
			name:           "RollbackWindow of 1 still leaves one block of margin",
			rollbackWindow: 1,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 99_999),
				contiguousStore("stateWAL", 100_000),
			},
			wantPruneBelow: ptr(99_999),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "RollbackWindow 0: cutLine equals head",
			rollbackWindow: 0,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 90_000),
				contiguousStore("stateWAL", 100_000),
			},
			// cutLine 100_000; sc 90_000; WAL 100_000 → min 90_000.
			wantPruneBelow: ptr(90_000),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "positive contiguous retention deepens that store's cut line",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 90_000),
				withRetentionWindow(contiguousStore("stateWAL", 100_000), 5_000),
			},
			// sc cutLine 90_000 → 90_000; WAL cutLine 85_000 → 85_000 → min 85_000.
			wantPruneBelow: ptr(85_000),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "SS retention 0 behaves like SC",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("ss", 100_000, 80_000, 90_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantPruneBelow: ptr(90_000),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "infinite retention on every store skips pruning",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				withRetentionWindow(contiguousStore("blockDB", 100_000), InfiniteRetentionWindow),
				withRetentionWindow(contiguousStore("receiptDB", 100_000), InfiniteRetentionWindow),
			},
			wantPruneBelow: nil,
			wantPruned:     []bool{false, false},
		},
		{
			name:           "infinite retention on one store leaves others free to prune",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				withRetentionWindow(contiguousStore("archiveWAL", 100_000), InfiniteRetentionWindow),
				snapshotStore("sc", 100_000, 80_000),
				contiguousStore("stateWAL", 100_000),
			},
			// archiveWAL skipped; sc 80_000; stateWAL 90_000 → min 80_000.
			wantPruneBelow: ptr(80_000),
			wantPruned:     []bool{false, true, true},
		},
		{
			name:           "snapshot exactly at the cut line is kept",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 50_000, 90_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantPruneBelow: ptr(90_000),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "all snapshots above cut line: store votes the cut line",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000, 97_000),
				contiguousStore("stateWAL", 100_000),
			},
			// Both answer 90_000 → shared prune 90_000 (sc's snapshots sit above it, untouched).
			wantPruneBelow: ptr(90_000),
			wantPruned:     []bool{true, true},
		},
		{
			// Regression: with no contiguous store to bound the min, an answer above cutLine
			// would become pruneHeight and delete inside the rollback window.
			name:           "all snapshots above cut line with no contiguous store",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000, 97_000),
			},
			wantPruneBelow: ptr(90_000),
			wantPruned:     []bool{true},
		},
		{
			name:           "lagging store lowers the global head",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("lagging", 50_000, 30_000, 50_000),
				contiguousStore("stateWAL", 100_000),
			},
			// head 50_000 → cutLine 40_000; lagging answers 30_000; WAL 40_000 → min 30_000.
			wantPruneBelow: ptr(30_000),
			wantPruned:     []bool{true, true},
		},
		{
			// A store with no snapshot will replay forward once its first one lands, so pruning
			// the WAL to its own cut line now would delete the range it replays from.
			name:           "store with no snapshot blocks the whole cycle",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantPruneBelow: nil,
			wantPruned:     []bool{false, false},
		},
		{
			// Same store set, but RollbackWindow 0 waives the guarantee, so there is nothing
			// left for the snapshot-less store to protect and the others prune.
			name:           "no snapshot does not block when RollbackWindow is 0",
			rollbackWindow: 0,
			stores: []*mockStore{
				snapshotStore("sc", 100_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantPruneBelow: ptr(100_000),
			wantPruned:     []bool{false, true},
		},
		{
			name:           "zero head ignored for global head; store still votes",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("stalled", 0, 50_000),
				contiguousStore("stateWAL", 100_000),
			},
			// head from WAL 100_000; stalled still answers 50_000 → min 50_000.
			wantPruneBelow: ptr(50_000),
			wantPruned:     []bool{true, true},
		},
		{
			name:           "head inside retain window: no prune",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 5_000, 1_000, 2_000),
				contiguousStore("stateWAL", 5_000),
			},
			wantPruneBelow: nil,
			wantPruned:     []bool{false, false},
		},
		{
			name:           "head inside one store's window skips only that store",
			rollbackWindow: 60_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 500, 1_000),
				withRetentionWindow(contiguousStore("stateWAL", 100_000), 40_000),
			},
			// WAL cutLine 0 (skipped); sc cutLine 40_000 → 1_000.
			wantPruneBelow: ptr(1_000),
			wantPruned:     []bool{true, false},
		},
		{
			name:           "no store has a latest block",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 0),
				contiguousStore("stateWAL", 0),
			},
			wantPruneBelow: nil,
			wantPruned:     []bool{false, false},
		},
		{
			name:           "no stores at all",
			rollbackWindow: 10_000,
			stores:         nil,
			wantPruneBelow: nil,
			wantPruned:     nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Len(t, tc.wantPruned, len(tc.stores), "wantPruned must have one entry per store")

			require.NoError(t, prune(testConfig(t, tc.rollbackWindow), prunableStores(tc.stores...)))

			for i, store := range tc.stores {
				if !tc.wantPruned[i] {
					require.Falsef(t, store.pruneBelowCalled.Load(), "%s should not be pruned", store.name)
					continue
				}
				require.NotNil(t, tc.wantPruneBelow, "wantPruned expects a prune height")
				require.Truef(t, store.pruneBelowCalled.Load(), "%s should be pruned", store.name)
				require.Equalf(t, *tc.wantPruneBelow, store.prunedBelow.Load(), "%s prune height", store.name)
			}
		})
	}
}

func TestPruneOnYoungChainWithDeepContiguousRetention(t *testing.T) {
	// head 100 << WAL retain window (rollback 1_000 + retention 100_000) → both cutLines 0.
	sc := snapshotStore("sc", 100, 50, 100)
	wal := withRetentionWindow(contiguousStore("stateWAL", 100), 100_000)

	require.NoError(t, prune(testConfig(t, 1_000), prunableStores(sc, wal)))

	require.False(t, sc.pruneBelowCalled.Load())
	require.False(t, wal.pruneBelowCalled.Load())
}

// Answers are held positionally rather than keyed by Name(), so a store sharing a name with a
// participating one must not inherit its prune. The stake is data loss: an infinite-retention
// store exists precisely to keep what the others are dropping.
func TestPruneKeepsInfiniteRetentionStoreWithDuplicateName(t *testing.T) {
	archive := withRetentionWindow(contiguousStore("ss", 100_000), InfiniteRetentionWindow)
	participating := snapshotStore("ss", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000)

	require.NoError(t, prune(testConfig(t, 10_000), prunableStores(archive, participating, wal)))

	require.False(t, archive.pruneBelowCalled.Load(), "an infinite-retention store must never be pruned")
	require.True(t, participating.pruneBelowCalled.Load())
	require.Equal(t, uint64(80_000), participating.prunedBelow.Load())
	require.Equal(t, uint64(80_000), wal.prunedBelow.Load())
}

func TestPruneGetLatestBlockError(t *testing.T) {
	sentinel := errors.New("boom")
	sc := snapshotStore("sc", 100_000, 80_000)
	broken := contiguousStore("brokenStore", 100_000)
	broken.getErr = sentinel

	err := prune(testConfig(t, 10_000), prunableStores(sc, broken))
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "brokenStore")
	require.False(t, sc.pruneBelowCalled.Load())
	require.False(t, broken.pruneBelowCalled.Load())
}

func TestPruneBelowErrorContinuesRemainingStores(t *testing.T) {
	firstErr := errors.New("boom1")
	secondErr := errors.New("boom2")
	first := snapshotStore("first", 100_000, 80_000)
	first.pruneErr = firstErr
	second := snapshotStore("second", 100_000, 80_000)
	second.pruneErr = secondErr
	wal := contiguousStore("stateWAL", 100_000)

	err := prune(testConfig(t, 10_000), prunableStores(first, second, wal))
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.ErrorContains(t, err, "first")
	require.ErrorContains(t, err, "second")
	require.ErrorContains(t, err, "80000")
	require.True(t, first.pruneBelowCalled.Load())
	require.True(t, second.pruneBelowCalled.Load())
	require.True(t, wal.pruneBelowCalled.Load())
	require.Equal(t, uint64(80_000), wal.prunedBelow.Load())
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
		{name: "zero rollback window", head: 100_000, rollbackWindow: 0, want: 100_000},
		{name: "zero rollback with retention", head: 100_000, rollbackWindow: 0, retention: 10_000, want: 90_000},
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
		{name: "rollback plus retention overflows uint64", head: math.MaxUint64, rollbackWindow: math.MaxUint64, retention: 1, want: 0},
		{name: "max rollback alone does not overflow", head: math.MaxUint64, rollbackWindow: math.MaxUint64, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, getCutLine(tc.head, tc.rollbackWindow, tc.retention))
		})
	}
}

func TestGetGlobalLatestBlock(t *testing.T) {
	storesWithHeads := func(heads ...uint64) []PrunableStore {
		list := make([]*mockStore, len(heads))
		for i, head := range heads {
			list[i] = contiguousStore("store", head)
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
			head, err := getGlobalLatestBlock(storesWithHeads(tc.heads...))
			require.NoError(t, err)
			require.Equal(t, tc.want, head)
		})
	}
}

// The prune log is the stated mechanism for reconstructing a deletion after the fact, so the three
// outcomes must not collapse onto the same rendering: never asked (cutLine 0), unable to serve a
// rollback, and asked with a boundary. The first two both carry boundary 0, so cutLine is the only
// thing separating them.
func TestDescribeDecisionsRendersEachOutcomeDistinctly(t *testing.T) {
	stores := prunableStores(
		withRetentionWindow(contiguousStore("archiveWAL", 100_000), InfiniteRetentionWindow),
		snapshotStore("sc", 100_000),
		contiguousStore("stateWAL", 100_000),
	)
	decisions := []storeDecision{
		{cutLine: 0, boundary: CannotServeRollback},      // skipped before being asked
		{cutLine: 90_000, boundary: CannotServeRollback}, // asked, cannot serve a rollback
		{cutLine: 90_000, boundary: 90_000},              // asked, reported a boundary
	}

	require.Equal(t,
		"archiveWAL=notAsked sc=cannotServeRollback(cutLine=90000) "+
			"stateWAL=90000(cutLine=90000)",
		describeDecisions(stores, decisions),
	)
}

// One blocker abandons the cycle, so the loop stops asking — but only when a rollback window is
// actually in force. Under RollbackWindow 0 the guarantee is waived and the blocker is ignored,
// which means the later stores must still be asked or the waiver has nothing left to prune.
func TestPruneStopsAtFirstBlockerUnlessWaived(t *testing.T) {
	blocker := snapshotStore("sc", 100_000) // no snapshot -> CannotServeRollback
	afterBlocker := contiguousStore("stateWAL", 100_000)
	require.NoError(t, prune(testConfig(t, 1_000), prunableStores(blocker, afterBlocker)))

	require.Equal(t, uint64(1), blocker.boundaryCalls.Load())
	require.False(t, afterBlocker.pruneBelowCalled.Load(), "the cycle must be abandoned")
	require.Zero(t, afterBlocker.boundaryCalls.Load(), "one blocker is enough; stop asking")

	waived := snapshotStore("sc", 100_000)
	afterWaived := contiguousStore("stateWAL", 100_000)
	require.NoError(t, prune(testConfig(t, 0), prunableStores(waived, afterWaived)))

	require.Equal(t, uint64(1), afterWaived.boundaryCalls.Load(),
		"RollbackWindow 0 ignores the blocker, so later stores must still be asked")
	require.True(t, afterWaived.pruneBelowCalled.Load())
	require.Equal(t, uint64(100_000), afterWaived.prunedBelow.Load())
}

func TestDefaultStorageGarbageCollectorConfig(t *testing.T) {
	cfg := DefaultStorageGarbageCollectorConfig()
	require.Equal(t, uint64(1_000), cfg.RollbackWindow)
	require.Equal(t, 5*time.Minute, cfg.PruneInterval)
	require.NoError(t, cfg.Validate())
}

func TestValidate(t *testing.T) {
	require.ErrorContains(t, (*StorageGarbageCollectorConfig)(nil).Validate(), "config is required")

	require.NoError(t, (&StorageGarbageCollectorConfig{
		RollbackWindow: 0,
		PruneInterval:  time.Minute,
	}).Validate())

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

// A nil ctx must be rejected here rather than panicking later on run()'s goroutine, where the
// panic is unrecoverable and takes the process down.
func TestNewStorageGarbageCollectorNilContext(t *testing.T) {
	//nolint:staticcheck // SA1012: passing a nil ctx is the case under test.
	sm, err := NewStorageGarbageCollector(nil, DefaultStorageGarbageCollectorConfig(), nil)
	require.ErrorContains(t, err, "context is required")
	require.Nil(t, sm)
}

func TestNewStorageGarbageCollectorConstructAndClose(t *testing.T) {
	sm, err := NewStorageGarbageCollector(
		context.Background(),
		DefaultStorageGarbageCollectorConfig(),
		prunableStores(snapshotStore("sc", 100, 100), contiguousStore("stateWAL", 100)),
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

// startCollector runs a real collector on a short PruneInterval so the ticker branch in run() is exercised.
func startCollector(t *testing.T, interval time.Duration, stores ...*mockStore) {
	t.Helper()

	config := &StorageGarbageCollectorConfig{RollbackWindow: 10_000, PruneInterval: interval}
	require.NoError(t, config.Validate())

	sm, err := NewStorageGarbageCollector(context.Background(), config, prunableStores(stores...))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sm.Close()) })
}

// TestRunTickerDrivesPruneCycles covers run()'s ticker branch: a cycle fires on its own and prunes to the expected
// height.
func TestRunTickerDrivesPruneCycles(t *testing.T) {
	sc := snapshotStore("sc", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000)

	startCollector(t, 10*time.Millisecond, sc, wal)

	require.Eventually(t, func() bool {
		return sc.pruneBelowCalled.Load() && wal.pruneBelowCalled.Load()
	}, 2*time.Second, 5*time.Millisecond, "ticker should drive a prune cycle")
	require.Equal(t, uint64(80_000), sc.prunedBelow.Load())
	require.Equal(t, uint64(80_000), wal.prunedBelow.Load())
}

// TestRunSurvivesPruneError covers run()'s logger.Error branch: a PruneBelow failure is logged and the loop keeps
// ticking rather than exiting after the first error.
func TestRunSurvivesPruneError(t *testing.T) {
	broken := snapshotStore("broken", 100_000, 80_000)
	broken.pruneErr = errors.New("boom")

	startCollector(t, 10*time.Millisecond, broken)

	require.Eventually(t, func() bool {
		return broken.pruneBelowCalls.Load() > 1
	}, 2*time.Second, 5*time.Millisecond, "a failed prune must not kill the run loop")
}

func ptr(v uint64) *uint64 {
	return &v
}
