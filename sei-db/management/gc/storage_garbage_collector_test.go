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
	floor        func(rollbackWindow uint64) uint64
	pruneErr     error
	// selfPruned inverts ExternalPruning: the constructors below build collector-managed stores,
	// which is the common case, so opting out is what a test has to say explicitly.
	selfPruned bool

	historyPruned      atomic.Bool
	prunedHistoryBelow atomic.Uint64
	historyPruneCalls  atomic.Uint64

	snapshotsPruned      atomic.Bool
	prunedSnapshotsBelow atomic.Uint64

	floorCalls atomic.Uint64
}

// snapshotStore models SC/SS, mirroring flatkv's snapshotFloor: the newest snapshot at or below its
// own head less the window, or its oldest snapshot when every one of them is above that height, and 0
// when it holds no snapshot or the window is deeper than its head. Snapshots must be ascending.
func snapshotStore(name string, latestHeight uint64, snapshots ...uint64) *mockStore {
	return &mockStore{
		name:         name,
		latestHeight: latestHeight,
		floor: func(rollbackWindow uint64) uint64 {
			if len(snapshots) == 0 || latestHeight <= rollbackWindow {
				return 0
			}
			target := latestHeight - rollbackWindow
			if snapshots[0] > target {
				return snapshots[0]
			}
			newest := snapshots[0]
			for _, snapshot := range snapshots {
				if snapshot > target {
					break
				}
				newest = snapshot
			}
			return newest
		},
	}
}

// contiguousStore models blockDB / receiptDB / WAL: it can restore to any height it holds, so it
// answers its own head less the window — and 0 where the window is deeper than that head, which
// includes the empty store and holds both cut lines at 0.
func contiguousStore(name string, latestHeight uint64) *mockStore {
	return &mockStore{
		name:         name,
		latestHeight: latestHeight,
		floor: func(rollbackWindow uint64) uint64 {
			if latestHeight <= rollbackWindow {
				return 0
			}
			return latestHeight - rollbackWindow
		},
	}
}

// withSelfPruning marks a store as enforcing its own retention, so the collector must count its
// answer but never prune it.
func withSelfPruning(store *mockStore) *mockStore {
	store.selfPruned = true
	return store
}

func (m *mockStore) ExternalPruning() bool {
	return !m.selfPruned
}

func (m *mockStore) Name() string {
	return m.name
}

func (m *mockStore) GetLatestBlock() (uint64, error) {
	return m.latestHeight, nil
}

func (m *mockStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	m.floorCalls.Add(1)
	return m.floor(rollbackWindow)
}

func (m *mockStore) PruneHistory(blockNumber uint64) error {
	m.historyPruned.Store(true)
	m.prunedHistoryBelow.Store(blockNumber)
	m.historyPruneCalls.Add(1)
	return m.pruneErr
}

func (m *mockStore) PruneSnapshots(blockNumber uint64) error {
	m.snapshotsPruned.Store(true)
	m.prunedSnapshotsBelow.Store(blockNumber)
	return m.pruneErr
}

func prunableStores(list ...*mockStore) []PrunableStore {
	result := make([]PrunableStore, len(list))
	for i, store := range list {
		result[i] = store
	}
	return result
}

func testConfig(t *testing.T, rollbackWindow uint64, lookbackWindow int64) *StorageGarbageCollectorConfig {
	t.Helper()
	config := &StorageGarbageCollectorConfig{
		RollbackWindow: rollbackWindow,
		LookbackWindow: lookbackWindow,
		PruneInterval:  time.Minute,
	}
	require.NoError(t, config.Validate())
	return config
}

// TestPruneDecisions covers one prune cycle. wantHistoryBelow == nil means no store is touched at
// all; otherwise every collector-managed store is pruned to that height. The two are separate
// because a cycle can reach one cut line and not the other: the lookback window can take the history
// cut line below genesis while snapshots are still prunable.
//
// Expectations are hardcoded per case rather than derived from getHistoryCutLine /
// GetRollbackFloor: deriving them from the code under test would let a sign error or off-by-one
// shift the expectation in lockstep with the bug.
func TestPruneDecisions(t *testing.T) {
	cases := []struct {
		name               string
		rollbackWindow     uint64
		lookbackWindow     int64
		stores             []*mockStore
		wantSnapshotsBelow *uint64
		wantHistoryBelow   *uint64
	}{
		{
			name:           "SC and WAL: min floor wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc keeps snapshot 85_000; WAL answers 90_000; min 85_000, no lookback to subtract.
			wantSnapshotsBelow: ptr(85_000),
			wantHistoryBelow:   ptr(85_000),
		},
		{
			name:           "lowest floor across many stores wins",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 85_000, 92_000),
				snapshotStore("ss", 100_000, 70_000, 88_000, 95_000),
				snapshotStore("flatKV", 100_000, 50_000, 90_000, 99_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc 85_000, ss 88_000, flatKV 90_000, WAL 90_000 → min 85_000.
			wantSnapshotsBelow: ptr(85_000),
			wantHistoryBelow:   ptr(85_000),
		},
		{
			// The lookback window deepens history without deepening the snapshot depth: restore
			// points are only needed as far back as the rollback promise.
			name:           "lookback window deepens history only",
			rollbackWindow: 10_000,
			lookbackWindow: 5_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc's only snapshot is above head - 10_000, so it answers that snapshot, 95_000; WAL
			// 90_000; min 90_000, and history goes 5_000 deeper.
			wantSnapshotsBelow: ptr(90_000),
			wantHistoryBelow:   ptr(85_000),
		},
		{
			// The lookback window is subtracted from the minimum however deep that minimum already
			// is, so it stacks below a snapshot floor rather than being capped by the head.
			name:           "lookback window stacks below the lowest floor",
			rollbackWindow: 10_000,
			lookbackWindow: 5_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 60_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc holds a snapshot at 60_000, which binds; 5_000 blocks of lookback go below it.
			wantSnapshotsBelow: ptr(60_000),
			wantHistoryBelow:   ptr(55_000),
		},
		{
			name:           "lookback window alone bounds a contiguous fleet",
			rollbackWindow: 1_000,
			lookbackWindow: 50_000,
			stores: []*mockStore{
				contiguousStore("blockDB", 100_000),
				contiguousStore("receiptDB", 100_000),
			},
			// Both answer 99_000; history goes 50_000 deeper.
			wantSnapshotsBelow: ptr(99_000),
			wantHistoryBelow:   ptr(49_000),
		},
		{
			name:           "RollbackWindow of 1 still leaves one block of margin",
			rollbackWindow: 1,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 99_999),
				contiguousStore("stateWAL", 100_000),
			},
			wantSnapshotsBelow: ptr(99_999),
			wantHistoryBelow:   ptr(99_999),
		},
		{
			name:           "both windows 0: nothing is held back but the snapshot",
			rollbackWindow: 0,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 80_000, 90_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc 90_000; WAL 100_000 → min 90_000.
			wantSnapshotsBelow: ptr(90_000),
			wantHistoryBelow:   ptr(90_000),
		},
		{
			name:           "snapshot exactly at the window depth is kept",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 50_000, 90_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantSnapshotsBelow: ptr(90_000),
			wantHistoryBelow:   ptr(90_000),
		},
		{
			// The shortfall case: SC cannot restore as deep as the window asks, so it answers its
			// oldest snapshot to keep that one replayable rather than waiving its claim.
			name:           "all snapshots above the window: store answers its oldest",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000, 97_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc 95_000; WAL 90_000 → min 90_000.
			wantSnapshotsBelow: ptr(90_000),
			wantHistoryBelow:   ptr(90_000),
		},
		{
			// The shortfall alone, with no contiguous store to bind the minimum. 95_000 sits above
			// head - RollbackWindow, and nothing caps it: the store cannot restore below its oldest
			// snapshot, so history below that buys a rollback no store could serve anyway.
			name:           "a shortfall answer alone sets the cut lines",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 95_000, 97_000),
			},
			wantSnapshotsBelow: ptr(95_000),
			wantHistoryBelow:   ptr(95_000),
		},
		{
			// Each store measures the window against its own head, so the WAL answers 90_000 off
			// its own 100_000 while the lagging store answers 30_000 off its 50_000. The lagging
			// store cannot be pruned past what it holds.
			name:           "each store answers from its own head",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("lagging", 50_000, 30_000, 50_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantSnapshotsBelow: ptr(30_000),
			wantHistoryBelow:   ptr(30_000),
		},
		{
			// A snapshot store with nothing to replay from cannot say which blocks the WAL may
			// drop, so it answers 0 and the whole fleet stays where it is.
			name:           "store with no snapshot holds both cut lines at 0",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 100_000),
				contiguousStore("stateWAL", 100_000),
			},
		},
		{
			// A store that has ingested nothing is the same case: it answers 0, the minimum carries
			// that through, and nothing is deleted anywhere.
			name:           "empty store holds both cut lines at 0",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				contiguousStore("blockDB", 0),
				contiguousStore("stateWAL", 100_000),
			},
		},
		{
			// A snapshot on disk does not make a store prunable-around while its head is behind the
			// window: it answers from the head, so a stalled store holds everything back.
			name:           "stalled store with a snapshot still answers 0",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("stalled", 0, 50_000),
				contiguousStore("stateWAL", 100_000),
			},
		},
		{
			// Every store owes a rollback deeper than its whole history, so every one answers 0.
			name:           "head inside the rollback window: nothing deleted",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 5_000, 1_000, 2_000),
				contiguousStore("stateWAL", 5_000),
			},
		},
		{
			// The one case where the cycle reaches one cut line and not the other: the rollback
			// promise is satisfiable, so snapshots below 10_000 go, but the lookback window reaches
			// below genesis from there and history is left alone.
			name:           "lookback window reaches below genesis: snapshots only",
			rollbackWindow: 1_000,
			lookbackWindow: 90_000,
			stores: []*mockStore{
				snapshotStore("sc", 50_000, 10_000),
				contiguousStore("stateWAL", 50_000),
			},
			wantSnapshotsBelow: ptr(10_000),
		},
		{
			// -1 is infinite history retention: snapshots below the rollback floor are still
			// reclaimed, but history is never pruned however far that floor advances.
			name:           "infinite lookback: snapshots only, history untouched",
			rollbackWindow: 1_000,
			lookbackWindow: -1,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 10_000),
				contiguousStore("stateWAL", 100_000),
			},
			wantSnapshotsBelow: ptr(10_000),
		},
		{
			// Infinite lookback must not force a deletion on a chain younger than its rollback
			// window. Every store owes a rollback deeper than its whole history, answers 0, and the
			// minimum holds both cut lines there — the -1 that zeroes the history cut line finds it
			// already 0, so nothing is pruned anywhere.
			name:           "infinite lookback on a young chain: nothing deleted",
			rollbackWindow: 1_000,
			lookbackWindow: -1,
			stores: []*mockStore{
				snapshotStore("sc", 100, 50),
				contiguousStore("blockDB", 100),
				contiguousStore("stateWAL", 100),
			},
		},
		{
			// A fleet that keeps no snapshots — blockDB, receiptDB, WAL — still has a snapshot cut
			// line: the collector issues PruneSnapshots and each store no-ops it. Infinite lookback
			// holds every block of history above genesis.
			name:           "infinite lookback on a contiguous fleet: history untouched",
			rollbackWindow: 10_000,
			lookbackWindow: -1,
			stores: []*mockStore{
				contiguousStore("blockDB", 100_000),
				contiguousStore("receiptDB", 100_000),
				contiguousStore("stateWAL", 100_000),
			},
			// All three answer 90_000; the snapshot cut line fires as a no-op on stores that hold
			// none, and history is never pruned.
			wantSnapshotsBelow: ptr(90_000),
		},
		{
			// The mixed fleet the collector actually runs: SC holds the snapshots and binds the
			// snapshot cut line, while blockDB, receiptDB and the WAL answer from their own heads.
			// Infinite lookback keeps all history across every one of them.
			name:           "infinite lookback across SC, blockDB, receiptDB and WAL",
			rollbackWindow: 10_000,
			lookbackWindow: -1,
			stores: []*mockStore{
				snapshotStore("sc", 100_000, 50_000),
				contiguousStore("blockDB", 100_000),
				contiguousStore("receiptDB", 100_000),
				contiguousStore("stateWAL", 100_000),
			},
			// sc 50_000; the contiguous three 90_000 → min 50_000. History is never pruned.
			wantSnapshotsBelow: ptr(50_000),
		},
		{
			name:           "no store has a latest block",
			rollbackWindow: 10_000,
			stores: []*mockStore{
				snapshotStore("sc", 0),
				contiguousStore("stateWAL", 0),
			},
		},
		{
			name:           "no stores at all",
			rollbackWindow: 10_000,
			stores:         nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := testConfig(t, tc.rollbackWindow, tc.lookbackWindow)
			require.NoError(t, prune(config, prunableStores(tc.stores...)))

			for _, store := range tc.stores {
				if tc.wantSnapshotsBelow == nil {
					require.Falsef(t, store.snapshotsPruned.Load(), "%s snapshots should not be pruned", store.name)
				} else {
					require.Truef(t, store.snapshotsPruned.Load(), "%s snapshots should be pruned", store.name)
					require.Equalf(t, *tc.wantSnapshotsBelow, store.prunedSnapshotsBelow.Load(),
						"%s snapshot cut line", store.name)
				}

				if tc.wantHistoryBelow == nil {
					require.Falsef(t, store.historyPruned.Load(), "%s history should not be pruned", store.name)
					continue
				}
				require.Truef(t, store.historyPruned.Load(), "%s history should be pruned", store.name)
				require.Equalf(t, *tc.wantHistoryBelow, store.prunedHistoryBelow.Load(),
					"%s history cut line", store.name)
			}
		})
	}
}

// The whole reason PruneSnapshots and PruneHistory are separate calls: they are handed cut lines one
// lookback window apart. Collapsing them onto one height would either strand the retained snapshot
// (snapshots down at the history cut line are the restore points nothing can ask for) or throw away
// the lookback window (history stopping at the snapshot cut line).
func TestPruneUsesDifferentDepthsForSnapshotsAndHistory(t *testing.T) {
	sc := snapshotStore("sc", 100_000, 60_000)
	wal := contiguousStore("stateWAL", 100_000)

	require.NoError(t, prune(testConfig(t, 10_000, 5_000), prunableStores(sc, wal)))

	require.Equal(t, uint64(60_000), sc.prunedSnapshotsBelow.Load(),
		"sc's own snapshot binds the minimum, and it is never itself a candidate")
	require.Equal(t, uint64(55_000), wal.prunedHistoryBelow.Load(),
		"history goes a lookback window below that snapshot, keeping it replayable")
	require.Equal(t, uint64(60_000), wal.prunedSnapshotsBelow.Load(),
		"a contiguous store is still called; it holds no snapshots and no-ops")
}

// One cut line reaching genesis must not silence the other. Here the rollback promise is satisfiable
// so snapshots below the floor are dead weight and go, while the lookback window from that floor
// lands below genesis and history is left untouched.
func TestPruneStillPrunesSnapshotsWhenHistoryIsHeldAtZero(t *testing.T) {
	sc := snapshotStore("sc", 50_000, 10_000, 40_000)
	wal := contiguousStore("stateWAL", 50_000)

	require.NoError(t, prune(testConfig(t, 1_000, 90_000), prunableStores(sc, wal)))

	require.False(t, sc.historyPruned.Load(), "the lookback window reaches below genesis")
	require.True(t, sc.snapshotsPruned.Load(), "snapshots are still pruned")
	require.Equal(t, uint64(40_000), sc.prunedSnapshotsBelow.Load(),
		"sc's floor snapshot binds, so the 10_000 one below it goes")
}

// A chain younger than its own rollback window must lose nothing: every store owes a rollback
// deeper than its whole history, so every one answers 0 and both cut lines stay there. The
// deletions are still issued — they are no-ops at 0 — which is what keeps the collector free of a
// special case for the young chain.
func TestPruneOnYoungChain(t *testing.T) {
	sc := snapshotStore("sc", 100, 50, 100)
	wal := contiguousStore("stateWAL", 100)

	require.NoError(t, prune(testConfig(t, 1_000, 100_000), prunableStores(sc, wal)))

	require.Equal(t, uint64(0), sc.prunedHistoryBelow.Load())
	require.Equal(t, uint64(0), wal.prunedHistoryBelow.Load())
}

// Answers are held positionally rather than keyed by Name(), so a store sharing a name with another
// must not inherit its decision. The stake is data loss: only the blocked store's own answer says
// whether the range it needs still exists.
func TestPruneKeepsAnswersPositionalWithDuplicateNames(t *testing.T) {
	selfPruned := withSelfPruning(contiguousStore("ss", 100_000))
	participating := snapshotStore("ss", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000)

	require.NoError(t, prune(testConfig(t, 10_000, 0), prunableStores(selfPruned, participating, wal)))

	require.False(t, selfPruned.historyPruned.Load(), "a self-pruning store must never be pruned")
	require.True(t, participating.historyPruned.Load())
	require.Equal(t, uint64(80_000), participating.prunedHistoryBelow.Load())
	require.Equal(t, uint64(80_000), wal.prunedHistoryBelow.Load())
}

// A store that cannot read its own head has no separate error path to the collector: it answers a
// floor of 0, which the minimum carries into both cut lines, and the cycle deletes nothing anywhere.
// Refusing to delete is the whole requirement, and it falls out of the ordinary reduction.
func TestPruneUnreadableStoreStopsTheCycle(t *testing.T) {
	sc := snapshotStore("sc", 100_000, 80_000)
	unreadable := contiguousStore("brokenStore", 100_000)
	unreadable.floor = func(uint64) uint64 { return 0 }

	require.NoError(t, prune(testConfig(t, 10_000, 0), prunableStores(sc, unreadable)))

	require.False(t, sc.historyPruned.Load())
	require.False(t, sc.snapshotsPruned.Load())
	require.False(t, unreadable.historyPruned.Load())
}

func TestPruneErrorContinuesRemainingStores(t *testing.T) {
	firstErr := errors.New("boom1")
	secondErr := errors.New("boom2")
	first := snapshotStore("first", 100_000, 80_000)
	first.pruneErr = firstErr
	second := snapshotStore("second", 100_000, 80_000)
	second.pruneErr = secondErr
	wal := contiguousStore("stateWAL", 100_000)

	err := prune(testConfig(t, 10_000, 0), prunableStores(first, second, wal))
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.ErrorContains(t, err, "first")
	require.ErrorContains(t, err, "second")
	require.ErrorContains(t, err, "80000")
	require.True(t, first.historyPruned.Load())
	require.True(t, second.historyPruned.Load())
	require.True(t, wal.historyPruned.Load())
	require.Equal(t, uint64(80_000), wal.prunedHistoryBelow.Load())
}

// A failure pruning snapshots must not skip the history prune on the same store: the two are
// independent deletions, and permission-to-drop is not transactional.
func TestPruneHistoryStillRunsAfterSnapshotError(t *testing.T) {
	sentinel := errors.New("boom")
	sc := snapshotStore("sc", 100_000, 80_000)
	sc.pruneErr = sentinel

	err := prune(testConfig(t, 10_000, 0), prunableStores(sc))
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "snapshots below 80000")
	require.ErrorContains(t, err, "history below 80000")
	require.True(t, sc.historyPruned.Load())
}

func TestGetHistoryCutLine(t *testing.T) {
	cases := []struct {
		name            string
		snapshotCutLine uint64
		lookbackWindow  int64
		want            uint64
	}{
		{name: "no lookback leaves the cut line alone", snapshotCutLine: 90_000, want: 90_000},
		{name: "lookback deepens the cut line", snapshotCutLine: 90_000, lookbackWindow: 5_000, want: 85_000},
		{name: "one block of lookback", snapshotCutLine: 90_000, lookbackWindow: 1, want: 89_999},
		{name: "lookback one below the cut line", snapshotCutLine: 5_001, lookbackWindow: 5_000, want: 1},
		{name: "lookback exactly at the cut line", snapshotCutLine: 5_000, lookbackWindow: 5_000, want: 0},
		{name: "lookback past genesis", snapshotCutLine: 100, lookbackWindow: 100_000, want: 0},
		{name: "a cut line of 0 stays 0", snapshotCutLine: 0, lookbackWindow: 10, want: 0},
		{name: "no cut line and no lookback", want: 0},
		{name: "max cut line", snapshotCutLine: math.MaxUint64, lookbackWindow: 1, want: math.MaxUint64 - 1},
		{name: "infinite lookback holds history at 0", snapshotCutLine: math.MaxUint64, lookbackWindow: -1, want: 0},
		{name: "infinite lookback with no cut line", snapshotCutLine: 0, lookbackWindow: -1, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, getHistoryCutLine(tc.snapshotCutLine, tc.lookbackWindow))
		})
	}
}

// The prune log is the stated mechanism for reconstructing a deletion after the fact, so every store
// has to appear whatever it answered — including the one answering 0, which is the entry that explains
// a cycle that deleted nothing — and a store the collector does not prune has to be distinguishable
// from one it pruned to no effect.
func TestDescribeDecisionsRendersEveryStore(t *testing.T) {
	stores := prunableStores(
		contiguousStore("blockDB", 0),
		contiguousStore("stateWAL", 100_000),
		withSelfPruning(snapshotStore("selfSC", 100_000, 50_000)),
	)
	decisions := []storeDecision{
		{floor: 0, externalPruning: true},      // asked, needs everything kept
		{floor: 90_000, externalPruning: true}, // asked, named a floor
		{floor: 50_000},                        // asked, but prunes itself
	}

	require.Equal(t,
		"blockDB=0 stateWAL=90000 selfSC=50000(selfPruned)",
		describeDecisions(stores, decisions),
	)
}

// The whole point of ExternalPruning: a self-pruning store is still a full participant in the
// decision — its floor drags the shared minimum down and protects the range it replays from — while
// never being pruned by the collector. Withdrawing its answer instead would prune the WAL to 99_000
// and strand its snapshot at 50_000 with nothing to replay forward.
func TestPruneSkipsSelfPruningStoreButKeepsItsAnswer(t *testing.T) {
	selfPruned := withSelfPruning(snapshotStore("sc", 100_000, 50_000))
	wal := contiguousStore("stateWAL", 100_000)

	require.NoError(t, prune(testConfig(t, 1_000, 0), prunableStores(selfPruned, wal)))

	require.Equal(t, uint64(1), selfPruned.floorCalls.Load(), "a self-pruning store is still asked")
	require.False(t, selfPruned.historyPruned.Load(), "its own pruner enforces its retention")
	require.False(t, selfPruned.snapshotsPruned.Load(), "including its snapshots")
	require.True(t, wal.historyPruned.Load())
	require.Equal(t, uint64(50_000), wal.prunedHistoryBelow.Load(),
		"the self-pruning store's floor must still hold the WAL back to its snapshot")
}

// A self-pruning store that has ingested nothing still holds history where it is. Who deletes its
// data is a separate question from whether the range it will need exists yet, and only the second is
// what its floor reports.
func TestPruneSelfPruningEmptyStoreStillHoldsHistory(t *testing.T) {
	empty := withSelfPruning(contiguousStore("ss", 0))
	wal := contiguousStore("stateWAL", 100_000)

	require.NoError(t, prune(testConfig(t, 1_000, 0), prunableStores(empty, wal)))

	require.Equal(t, uint64(0), wal.prunedHistoryBelow.Load(), "history must not move past an empty store")
	require.False(t, empty.historyPruned.Load(), "its own pruner enforces its retention")
}

// Every store is asked exactly once per cycle, whatever the others answer, so the log carries a
// complete decision set and a store cannot be skipped on the strength of an earlier one's answer.
func TestPruneAsksEveryStoreOncePerCycle(t *testing.T) {
	empty := contiguousStore("blockDB", 0)
	sc := snapshotStore("sc", 100_000, 80_000)
	wal := contiguousStore("stateWAL", 100_000)

	require.NoError(t, prune(testConfig(t, 1_000, 0), prunableStores(empty, sc, wal)))

	for _, store := range []*mockStore{empty, sc, wal} {
		require.Equalf(t, uint64(1), store.floorCalls.Load(), "%s asked exactly once", store.name)
	}
	require.Equal(t, uint64(0), wal.prunedHistoryBelow.Load(), "the empty store's answer binds the cycle")
}

func TestDefaultStorageGarbageCollectorConfig(t *testing.T) {
	cfg := DefaultStorageGarbageCollectorConfig()
	require.Equal(t, uint64(1_000), cfg.RollbackWindow)
	require.Equal(t, int64(0), cfg.LookbackWindow)
	require.Equal(t, 5*time.Minute, cfg.PruneInterval)
	require.NoError(t, cfg.Validate())
}

// The windows are independent, so every combination of non-negative counts validates, as does the
// -1 infinite-retention sentinel on the lookback window. The interval and a lookback below -1 are
// the only rejections.
func TestValidate(t *testing.T) {
	require.ErrorContains(t, (*StorageGarbageCollectorConfig)(nil).Validate(), "config is required")

	for _, windows := range [][2]int64{{0, 0}, {1, 0}, {0, 1}, {1_000, 50_000}, {50_000, 1_000}, {1_000, -1}} {
		require.NoError(t, (&StorageGarbageCollectorConfig{
			RollbackWindow: uint64(windows[0]),
			LookbackWindow: windows[1],
			PruneInterval:  time.Minute,
		}).Validate(), "windows %v", windows)
	}

	require.ErrorContains(t, (&StorageGarbageCollectorConfig{
		RollbackWindow: 1,
		LookbackWindow: -2,
		PruneInterval:  time.Minute,
	}).Validate(), "lookback window")

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
		return sc.historyPruned.Load() && wal.historyPruned.Load()
	}, 2*time.Second, 5*time.Millisecond, "ticker should drive a prune cycle")
	require.Equal(t, uint64(80_000), sc.prunedHistoryBelow.Load())
	require.Equal(t, uint64(80_000), wal.prunedHistoryBelow.Load())
}

// TestRunSurvivesPruneError covers run()'s logger.Error branch: a prune failure is logged and the loop keeps
// ticking rather than exiting after the first error.
func TestRunSurvivesPruneError(t *testing.T) {
	broken := snapshotStore("broken", 100_000, 80_000)
	broken.pruneErr = errors.New("boom")

	startCollector(t, 10*time.Millisecond, broken)

	require.Eventually(t, func() bool {
		return broken.historyPruneCalls.Load() > 1
	}, 2*time.Second, 5*time.Millisecond, "a failed prune must not kill the run loop")
}

func ptr(v uint64) *uint64 {
	return &v
}
