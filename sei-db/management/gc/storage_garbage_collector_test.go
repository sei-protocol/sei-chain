package gc

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockStore is a PrunableStore with canned answers. Build one with snapshotStore or contiguousStore.
type mockStore struct {
	name         string
	latestHeight uint64
	// oldestToRetain answers GetOldestBlockToRetain for a given cut line.
	oldestToRetain func(cutLine uint64) uint64
	getErr         error
	pruneErr       error

	pruneBelowCalled bool
	prunedBelow      uint64
}

// snapshotStore models a store that can only be restored to a height it holds a snapshot for, such as SC or SS: it
// answers with the newest snapshot at or below the cut line, or with its oldest snapshot when every snapshot is above the
// cut line. Snapshots must be in ascending order; a store given none holds no data.
func snapshotStore(name string, latestHeight uint64, snapshots ...uint64) *mockStore {
	return &mockStore{
		name:         name,
		latestHeight: latestHeight,
		oldestToRetain: func(cutLine uint64) uint64 {
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

// contiguousStore models a store that retains every block in a range, such as blockDB, receiptDB or the state WAL. It can
// be restored to any block it holds, so it needs nothing below the cut line.
func contiguousStore(name string, latestHeight uint64, hasData bool) *mockStore {
	return &mockStore{
		name:         name,
		latestHeight: latestHeight,
		oldestToRetain: func(cutLine uint64) uint64 {
			if !hasData {
				return 0
			}
			return cutLine
		},
	}
}

func (m *mockStore) Name() string {
	return m.name
}

func (m *mockStore) GetLastCommittedBlock() (uint64, error) {
	return m.latestHeight, m.getErr
}

func (m *mockStore) GetOldestBlockToRetain(cutLine uint64) uint64 {
	return m.oldestToRetain(cutLine)
}

func (m *mockStore) PruneBelow(blockNumber uint64) error {
	m.pruneBelowCalled = true
	m.prunedBelow = blockNumber
	return m.pruneErr
}

// prunableStores adapts the mocks to the interface slice the collector holds.
func prunableStores(list ...*mockStore) []PrunableStore {
	result := make([]PrunableStore, len(list))
	for i, store := range list {
		result[i] = store
	}
	return result
}

// newTestCollector builds a collector without starting its run loop, so prune can be driven one cycle at a time.
func newTestCollector(
	t *testing.T,
	rollbackWindow uint64,
	storeRetention uint64,
	stores ...*mockStore,
) *StorageGarbageCollector {
	t.Helper()

	config := &StorageGarbageCollectorConfig{
		RollbackWindow:       rollbackWindow,
		StoreRetention:       storeRetention,
		PruneIntervalSeconds: 60,
	}
	// getCutLine subtracts without an overflow guard because Validate rejects a retain window that would overflow, so
	// test configs have to clear the same bar as real ones.
	require.NoError(t, config.Validate())

	return &StorageGarbageCollector{config: config, stores: prunableStores(stores...)}
}

// TestPruneDecisions is the decision matrix for a single prune cycle. wantPruneBelow == nil means no store may be pruned.
func TestPruneDecisions(t *testing.T) {
	cases := []struct {
		name           string
		rollbackWindow uint64
		storeRetention uint64
		stores         []*mockStore
		wantPruneBelow *uint64
	}{
		{
			name:           "one snapshotted store and a WAL",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// Cut line 90,000. The WAL needs only 90,000, but sc must keep the 85,000 snapshot a rollback to the cut
			// line would start from, so everything is pruned to 85,000. Pruning to the higher answer would delete
			// that snapshot.
			wantPruneBelow: ptr(85_000),
		},
		{
			name:           "the lowest need across many stores wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				snapshotStore("ss", 100_000, 70_000, 88_000, 95_000),
				snapshotStore("flatKV", 100_000, 50_000, 90_000, 99_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// Needs are 85,000, 88,000, 90,000 and 90,000.
			wantPruneBelow: ptr(85_000),
		},
		{
			name:           "zero rollback window prunes up to the head",
			rollbackWindow: 0,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 100_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(100_000),
		},
		{
			name:           "store retention deepens the cut line",
			rollbackWindow: 10_000,
			storeRetention: 5_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 90_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// Cut line 85,000 rather than 90,000, so the 80,000 snapshot is now the newest at or below it. Without the
			// extra retention sc would have needed only 90,000.
			wantPruneBelow: ptr(80_000),
		},
		{
			name:           "a snapshot exactly at the cut line is kept",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 50_000, 90_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(90_000),
		},
		{
			name:           "every snapshot above the cut line pins the store to its oldest",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000, 97_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// sc cannot drop either snapshot and answers 95,000, so the WAL's 90,000 is the lowest.
			wantPruneBelow: ptr(90_000),
		},
		{
			name:           "a lagging store pulls the cut line down with it",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("lagging", 50_000, 30_000, 50_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// The head is the lowest of the two, 50,000, putting the cut line at 40,000 and making 30,000 the newest
			// snapshot at or below it. Measuring from the WAL's head instead would put the cut line at 90,000, and the
			// 30,000 snapshot needed to roll back to 40,000 would be deleted.
			wantPruneBelow: ptr(30_000),
		},
		{
			name:           "a store holding nothing is ignored",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("empty", 100_000),
				snapshotStore("sc", 100_000, 80_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: ptr(80_000),
		},
		{
			name:           "a head of zero is ignored, the data behind it is not",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("stalled", 0, 50_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			// The stalled store is left out of the head, so the cut line comes from the WAL at 90,000. It still
			// reports that it needs its 50,000 snapshot, and that is the lowest answer.
			wantPruneBelow: ptr(50_000),
		},
		{
			name:           "chain younger than the retain window",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 5_000, 1_000, 2_000),
				contiguousStore("stateWAL", 5_000, true),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "head exactly at the retain window",
			rollbackWindow: 60_000,
			storeRetention: 40_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 500, 1_000),
				contiguousStore("stateWAL", 100_000, true),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "no store has committed a block",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 0),
				contiguousStore("stateWAL", 0, false),
			},
			wantPruneBelow: nil,
		},
		{
			name:           "stores are committing blocks but none retains any",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000),
				contiguousStore("stateWAL", 100_000, false),
			},
			// A fresh node whose stores have ingested blocks but have not yet written a first snapshot.
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
			collector := newTestCollector(t, tc.rollbackWindow, tc.storeRetention, tc.stores...)
			require.NoError(t, collector.prune())

			for _, store := range tc.stores {
				if tc.wantPruneBelow == nil {
					require.Falsef(t, store.pruneBelowCalled, "%s should not be pruned", store.name)
					continue
				}
				require.Truef(t, store.pruneBelowCalled, "%s should be pruned", store.name)
				require.Equalf(t, *tc.wantPruneBelow, store.prunedBelow, "%s prune height", store.name)
			}
		})
	}
}

// TestPruneOnYoungChainWithDefaultConfig guards the unsigned subtraction in getCutLine using the real defaults, where the
// retain window is 101,000 blocks. Every node spends its early life here, and a wrapped cut line would land far above the
// head of the chain and delete everything.
func TestPruneOnYoungChainWithDefaultConfig(t *testing.T) {
	sc := snapshotStore("sc", 100, 50, 100)
	wal := contiguousStore("stateWAL", 100, true)

	collector := &StorageGarbageCollector{
		config: DefaultStorageGarbageCollectorConfig(),
		stores: prunableStores(sc, wal),
	}
	require.NoError(t, collector.prune())

	require.False(t, sc.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

// TestPruneGetLastCommittedBlockError checks that a failure to read a head aborts the cycle before anything is deleted.
func TestPruneGetLastCommittedBlockError(t *testing.T) {
	sentinel := errors.New("boom")
	sc := snapshotStore("sc", 100_000, 80_000)
	broken := contiguousStore("brokenStore", 100_000, true)
	broken.getErr = sentinel

	err := newTestCollector(t, 10_000, 0, sc, broken).prune()
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "brokenStore")
	require.False(t, sc.pruneBelowCalled)
	require.False(t, broken.pruneBelowCalled)
}

// TestPruneBelowErrorStopsRemainingStores checks that the first failed deletion aborts the cycle, leaving later stores
// untouched.
func TestPruneBelowErrorStopsRemainingStores(t *testing.T) {
	sentinel := errors.New("boom")
	first := snapshotStore("first", 100_000, 80_000)
	first.pruneErr = sentinel
	second := snapshotStore("second", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000, true)

	err := newTestCollector(t, 10_000, 0, first, second, wal).prune()
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "first")
	require.ErrorContains(t, err, "80000") // the prune height, i.e. the lowest need across the stores
	require.True(t, first.pruneBelowCalled)
	require.False(t, second.pruneBelowCalled)
	require.False(t, wal.pruneBelowCalled)
}

func TestGetCutLine(t *testing.T) {
	cases := []struct {
		name           string
		head           uint64
		rollbackWindow uint64
		retention      uint64
		want           uint64
	}{
		{name: "rollback window only", head: 100_000, rollbackWindow: 10_000, want: 90_000},
		{name: "retention only", head: 100_000, retention: 10_000, want: 90_000},
		{name: "the two windows add", head: 100_000, rollbackWindow: 10_000, retention: 5_000, want: 85_000},
		{name: "no retain window at all", head: 100_000, want: 100_000},
		{name: "head one above the window", head: 10_001, rollbackWindow: 10_000, want: 1},
		{name: "head exactly at the window", head: 10_000, rollbackWindow: 10_000, want: 0},
		{name: "head one below the window", head: 9_999, rollbackWindow: 10_000, want: 0},
		// The default retain window against a young chain: the subtraction must not wrap.
		{name: "head far below the window", head: 100, rollbackWindow: 1_000, retention: 100_000, want: 0},
		{name: "head at genesis", head: 0, rollbackWindow: 10_000, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, getCutLine(tc.head, tc.rollbackWindow, tc.retention))
		})
	}
}

func TestGetGlobalLastCommittedBlock(t *testing.T) {
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
		// An uninitialized store must not drag the head of the chain down with it.
		{name: "leading zero ignored", heads: []uint64{0, 100}, want: 100},
		{name: "trailing zero ignored", heads: []uint64{100, 0}, want: 100},
		{name: "zero among many ignored", heads: []uint64{100, 0, 80}, want: 80},
		{name: "every store reports zero", heads: []uint64{0, 0}, want: 0},
		{name: "no stores", heads: nil, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, err := getGlobalLastCommittedBlock(storesWithHeads(tc.heads...))
			require.NoError(t, err)
			require.Equal(t, tc.want, head)
		})
	}
}

func TestDefaultStorageGarbageCollectorConfig(t *testing.T) {
	cfg := DefaultStorageGarbageCollectorConfig()
	require.Equal(t, uint64(1_000), cfg.RollbackWindow)
	require.Equal(t, uint64(100_000), cfg.StoreRetention)
	require.Equal(t, uint64(600), cfg.PruneIntervalSeconds)
	require.NoError(t, cfg.Validate())
}

func TestValidate(t *testing.T) {
	// A zero rollback window is legal, as is a zero store retention.
	require.NoError(t, (&StorageGarbageCollectorConfig{PruneIntervalSeconds: 60}).Validate())

	require.ErrorContains(t, (&StorageGarbageCollectorConfig{PruneIntervalSeconds: 0}).Validate(), "prune interval")

	// The largest interval that does not overflow time.Duration is accepted; one larger is rejected.
	maxInterval := uint64(math.MaxInt64) / uint64(time.Second)
	require.NoError(t, (&StorageGarbageCollectorConfig{PruneIntervalSeconds: maxInterval}).Validate())
	require.ErrorContains(t, (&StorageGarbageCollectorConfig{PruneIntervalSeconds: maxInterval + 1}).Validate(), "at most")

	// The retain window is the sum of the two, so the pair must not overflow. getCutLine subtracts it without a guard.
	require.NoError(t, (&StorageGarbageCollectorConfig{
		RollbackWindow:       math.MaxUint64,
		PruneIntervalSeconds: 60,
	}).Validate())
	require.ErrorContains(t, (&StorageGarbageCollectorConfig{
		RollbackWindow:       math.MaxUint64,
		StoreRetention:       1,
		PruneIntervalSeconds: 60,
	}).Validate(), "store retention")
}

func TestNewStorageGarbageCollectorInvalidConfig(t *testing.T) {
	sm, err := NewStorageGarbageCollector(
		context.Background(),
		&StorageGarbageCollectorConfig{PruneIntervalSeconds: 0},
		prunableStores(snapshotStore("sc", 100)),
	)
	require.Error(t, err)
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

	// Close must return without hanging.
	require.NoError(t, sm.Close())
}

func TestCloseAfterContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sm, err := NewStorageGarbageCollector(ctx, DefaultStorageGarbageCollectorConfig(), nil)
	require.NoError(t, err)

	// Cancelling the context lets the run loop exit on its own, so Close must still return.
	cancel()
	require.NoError(t, sm.Close())
}

func ptr(v uint64) *uint64 {
	return &v
}
