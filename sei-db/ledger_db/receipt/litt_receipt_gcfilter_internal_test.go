package receipt

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/stretchr/testify/require"
)

// floorStore returns a bare store whose retention floor is set, which is all gcFilter reads.
// Reclamation decisions are a pure function of that floor and the key, so nothing else is opened.
func floorStore(floor int64) *littReceiptStore {
	s := &littReceiptStore{}
	s.earliestVersion.Store(floor)
	return s
}

func requireFilter(t *testing.T, s *littReceiptStore, key []byte, isPrimary, want bool) {
	t.Helper()
	got, err := s.gcFilter(key, isPrimary)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// The point of the filter: a body at or above the floor is not reclaimable however old it is, so a
// TTL sized for the wrong window cannot delete what reads still serve.
func TestGCFilterHoldsBodiesAtOrAboveTheFloor(t *testing.T) {
	s := floorStore(100)

	requireFilter(t, s, littPartKey(99, 0), true, true)
	requireFilter(t, s, littPartKey(100, 0), true, false)
	requireFilter(t, s, littPartKey(101, 0), true, false)
}

// Every part of a block shares its fate, since the floor is a block boundary and a partially
// reclaimed block would serve some of its receipts and not others.
func TestGCFilterTreatsEveryPartOfABlockAlike(t *testing.T) {
	s := floorStore(100)

	for _, part := range []uint32{0, 1, 7} {
		requireFilter(t, s, littPartKey(99, part), true, true)
		requireFilter(t, s, littPartKey(100, part), true, false)
	}
}

// Tx-hash secondaries alias a body in their own segment, so the body's part key is what gates the
// segment. A secondary that blocked on its own would pin segments by tx hash, which carries no
// block number to compare against the floor.
func TestGCFilterPassesSecondaryKeys(t *testing.T) {
	s := floorStore(100)
	requireFilter(t, s, make([]byte, 32), false, true)
}

// An unestablished floor must block rather than permit: 0 means nothing has been pruned yet, or
// the index has not been read, and reclaiming on that reading is not recoverable.
func TestGCFilterBlocksEverythingWithoutAFloor(t *testing.T) {
	for _, floor := range []int64{0, -1} {
		s := floorStore(floor)
		requireFilter(t, s, littPartKey(0, 0), true, false)
		requireFilter(t, s, littPartKey(1_000_000, 0), true, false)
	}
}

// litt requires the filter to be monotonic: once true for a key, always true. The floor only
// advances, so this holds — assert it across an advancing floor rather than trusting the field.
func TestGCFilterIsMonotonicAsTheFloorAdvances(t *testing.T) {
	s := floorStore(0)
	key := littPartKey(50, 0)

	var everTrue bool
	for _, floor := range []int64{0, 10, 50, 51, 500} {
		s.earliestVersion.Store(floor)
		got, err := s.gcFilter(key, true)
		require.NoError(t, err)
		if everTrue {
			require.True(t, got, "the filter went back on a key it already released at floor %d", floor)
		}
		everTrue = everTrue || got
	}
	require.True(t, everTrue, "an advancing floor must eventually release the key")
}

// gcFilterTable builds a real litt table wired to s.gcFilter, sized so reclamation is observable in
// a unit test: segments seal on every write, and the TTL is already expired the moment a segment is
// sealed. That leaves the retention floor as the only thing deciding what may be reclaimed, which
// is the property under test.
//
// The receipt store's own segment thresholds (512MB) never seal in a test, so this drives litt
// directly rather than through newLittReceiptStore. What it cannot prove is that the constructor
// attaches the filter; the tests above and the wiring at the BuildTable call cover that separately.
func gcFilterTable(t *testing.T, s *littReceiptStore) litt.ManagedTable {
	t.Helper()

	cfg, err := litt.DefaultConfig(t.TempDir())
	require.NoError(t, err)
	cfg.TargetSegmentFileSize = 1 // seal on every write, so each block lands in a collectable segment
	cfg.GCPeriod = time.Hour      // only the explicit RunGC below collects, so nothing races a timer
	cfg.Fsync = false

	db, err := littbuilder.NewDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	tableConfig := litt.DefaultTableConfig(littReceiptTableName)
	tableConfig.ShardingFactor = 1
	tableConfig.TTL = time.Nanosecond // the age gate is satisfied at once; only the floor is left
	tableConfig.GCFilter = s.gcFilter
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	// RunGC lives on ManagedTable; without it a pass only happens on GCPeriod, which this test
	// deliberately pushes out of reach so the assertions are not racing a timer.
	managed, ok := table.(litt.ManagedTable)
	require.True(t, ok, "forcing a GC pass requires a ManagedTable")
	return managed
}

func requireBlockRetained(t *testing.T, table litt.Table, block uint64) {
	t.Helper()
	ok, err := table.Exists(littPartKey(block, 0))
	require.NoError(t, err)
	require.True(t, ok, "block %d was reclaimed while the retention floor still covered it", block)
}

// Reclamation is not synchronous with a GC pass: the pass schedules keymap deletes and the control
// loop drops the files once they are durable. Poll rather than assert straight after RunGC.
func requireBlockReclaimed(t *testing.T, table litt.Table, block uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		ok, err := table.Exists(littPartKey(block, 0))
		require.NoError(t, err)
		return !ok
	}, 5*time.Second, 10*time.Millisecond, "block %d stayed readable after the floor passed it", block)
}

// The end of the chain this change exists to close: pruning by block number is what releases receipt
// bodies to litt, and an expired TTL alone does not. Without the filter every block here would be
// reclaimable from the very first pass, since the TTL expires the moment a segment seals.
//
// The two directions are not symmetric, and the test asserts them at different strengths on purpose.
// Retention is exact — a block at or above the floor must never be reclaimed, and that is the
// guarantee the collector depends on. Reclamation is only eventual and segment-granular: litt frees
// whole segments, so a segment holding one block above the floor keeps every block below it in that
// same segment. Blocks just under the floor may therefore survive a pass, which is why the
// reclaimed-side assertion leaves a margin rather than pinning the block after the floor.
func TestGCFilterMakesLittReclamationFollowTheBlockFloor(t *testing.T) {
	const (
		head   = 12
		floor  = 10
		margin = 5 // comfortably below any segment that could straddle the floor
	)

	s := floorStore(0)
	table := gcFilterTable(t, s)

	for block := uint64(1); block <= head; block++ {
		require.NoError(t, table.Put(littPartKey(block, 0), []byte{byte(block)}))
	}
	require.NoError(t, table.Flush())

	// No floor established and the TTL long expired: nothing may go. This is the case the filter
	// exists for — every one of these is TTL-reclaimable and only the floor is holding it.
	require.NoError(t, table.RunGC())
	for block := uint64(1); block <= head; block++ {
		requireBlockRetained(t, table, block)
	}

	// Pruning by block number is what releases them.
	s.earliestVersion.Store(floor)
	require.NoError(t, table.RunGC())

	for block := uint64(1); block <= margin; block++ {
		requireBlockReclaimed(t, table, block)
	}
	for block := uint64(floor); block <= head; block++ {
		requireBlockRetained(t, table, block)
	}
}

// A primary key that is not a part key means the table's key layout changed without this filter
// being updated. Guessing a block number out of it would reclaim by coincidence, so litt's contract
// (an error crashes the DB loudly) is the right response.
func TestGCFilterRejectsAnUnknownPrimaryKey(t *testing.T) {
	s := floorStore(100)
	_, err := s.gcFilter(make([]byte, 32), true)
	require.ErrorContains(t, err, "unexpected primary receipt key length")
}
