package blockstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// collectorBuilder returns a store the collector's surface can be exercised on,
// as the concrete type rather than types.BlockStore: the prune surface is the
// collector's contract, not the consensus layer's, so it is deliberately absent
// from the latter.
type collectorBuilder func(t *testing.T) *blockstore.Store

func collectorImpls() []struct {
	name  string
	build collectorBuilder
} {
	return []struct {
		name  string
		build collectorBuilder
	}{
		{"memblock", func(t *testing.T) *blockstore.Store {
			store, err := blockstore.New(memblock.NewBlockDB())
			require.NoError(t, err)
			return store
		}},
		{"littblock", func(t *testing.T) *blockstore.Store {
			db, err := littblock.NewBlockDB(littConfig(t, t.TempDir()))
			require.NoError(t, err)
			store, err := blockstore.New(db)
			require.NoError(t, err)
			return store
		}},
	}
}

// TestCollectorSurface exercises the controller.PrunableStore half of the store's
// contract, which the storage garbage collector drives.
func TestCollectorSurface(t *testing.T) {
	for _, impl := range collectorImpls() {
		t.Run(impl.name, func(t *testing.T) {
			t.Run("EmptyStore", func(t *testing.T) { testCollectorEmptyStore(t, impl.build) })
			t.Run("LatestBlock", func(t *testing.T) { testCollectorLatestBlock(t, impl.build) })
			t.Run("RollbackFloor", func(t *testing.T) { testCollectorRollbackFloor(t, impl.build) })
			t.Run("PruneHistory", func(t *testing.T) { testCollectorPruneHistory(t, impl.build) })
			t.Run("PruneSnapshotsIsANoOp", func(t *testing.T) { testCollectorPruneSnapshots(t, impl.build) })
		})
	}
}

func testCollectorEmptyStore(t *testing.T, build collectorBuilder) {
	db := build(t)
	defer func() { _ = db.Close() }()

	require.NotEmpty(t, db.Name(), "the collector reports the store by name")
	require.True(t, db.ExternalPruning(), "this store reclaims only what the collector released")

	latest, err := db.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "a store that has ingested nothing has no head")
	require.Equal(t, uint64(0), db.GetRollbackFloor(0))
	require.Equal(t, uint64(0), db.GetRollbackFloor(42))

	// The collector prunes every store to a shared minimum, so a store holding
	// nothing is still asked. Answering with an error would fail a cycle that has
	// nothing to do with this store.
	require.NoError(t, db.PruneHistory(1_000))
	require.NoError(t, db.PruneSnapshots(1_000))
	require.Equal(t, uint64(0), uint64(db.First()), "a prune before the first write must not move the floor")
}

func testCollectorLatestBlock(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)

	head := batches[len(batches)-1].next - 1
	latest, err := db.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(head), latest, "the head is the newest block written")
}

func testCollectorRollbackFloor(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)
	head := uint64(batches[len(batches)-1].next - 1)

	require.Equal(t, head, db.GetRollbackFloor(0), "the whole store is inside a window of 0")
	require.Equal(t, head-1, db.GetRollbackFloor(1))
	// A window deeper than this store's own head is a rollback promise reaching
	// past genesis, so nothing here is eligible for pruning.
	require.Equal(t, uint64(0), db.GetRollbackFloor(head+1))
}

func testCollectorPruneHistory(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRng(), keys, batches)

	// PruneHistory is the collector's spelling of PruneBefore, so the floor it
	// settles on is bound by the same rules: rounded down to a cohort start and
	// capped so the store never empties.
	require.NoError(t, db.PruneHistory(uint64(batches[1].first)))
	require.Equal(t, batches[1].first, db.First())
}

func testCollectorPruneSnapshots(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRng(), keys, batches)

	// This store keeps no snapshots, so its half of the collector's split contract
	// does nothing. It is still called every cycle.
	require.NoError(t, db.PruneSnapshots(uint64(batches[1].first)))
	require.Equal(t, uint64(0), uint64(db.First()), "a snapshot prune must not move the history floor")
}
