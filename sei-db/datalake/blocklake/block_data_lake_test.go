package blocklake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// writeRoads writes the block, AppProposal and AppQC of roads [from, to) to store, then flushes it.
func writeRoads(t *testing.T, rng utils.Rng, store *blockstore.Store, from, to types.RoadHeight) {
	t.Helper()
	for road := from; road < to; road++ {
		laneBlock := types.GenLaneBlock(rng)
		qc := types.GenFullCommitQCAt(rng, road, 1)
		qc = types.NewFullCommitQC(qc.QC(), []*types.LaneBlockHeader{laneBlock.Header()})
		require.NoError(t, store.WriteBlock(types.NewRoadBlock(qc, []*types.LaneBlock{laneBlock})))
		appProposal := types.GenAppProposalAt(rng, road)
		require.NoError(t, store.WriteAppProposal(appProposal))
		require.NoError(t, store.WriteAppQC(types.GenAppQCFor(rng, appProposal)))
	}
	require.NoError(t, store.Flush())
}

// waitForCopies waits until every store of lake has made every road below next durable.
func waitForCopies(t *testing.T, lake *BlockDataLake, next types.RoadHeight) {
	t.Helper()
	for _, m := range lake.mirrors {
		require.Eventually(t, func() bool { return m.lake.GetDurableWatermark() >= uint64(next) },
			time.Minute, time.Millisecond, m.kind.storeName)
	}
}

// requireCopies checks that every store of lake holds exactly roads [first, next), each byte for byte as store holds
// it.
func requireCopies(t *testing.T, lake *BlockDataLake, store *blockstore.Store, first, next types.RoadHeight) {
	t.Helper()
	for _, m := range lake.mirrors {
		firstKey, ok := m.lake.GetFirstKey()
		require.True(t, ok, m.kind.storeName)
		require.Equal(t, uint64(first), firstKey, m.kind.storeName)
		require.Equal(t, uint64(next), m.lake.GetDurableWatermark(), m.kind.storeName)
		for road := first; road < next; road++ {
			want, err := m.kind.waitForRecord(store, t.Context(), road)
			require.NoError(t, err)
			got, ok, err := m.lake.Get(t.Context(), uint64(road))
			require.NoError(t, err)
			require.True(t, ok, "%s road %d", m.kind.storeName, road)
			require.Equal(t, want, got, "%s road %d", m.kind.storeName, road)
		}
	}
}

func TestCopiesEveryDurableRecordByteForByte(t *testing.T) {
	rng := utils.TestRng()
	store := blockstore.NewTestStore(t)
	writeRoads(t, rng, store, 1, 4)

	lake, err := New(t.Context(), cdkdatalake.DefaultTestDataLakeConfig(t.TempDir()), store)
	require.NoError(t, err)
	defer func() { require.NoError(t, lake.Close()) }()
	writeRoads(t, rng, store, 4, 8)

	waitForCopies(t, lake, 8)
	requireCopies(t, lake, store, 1, 8)
}

func TestWaitsForAnEmptyBlockStore(t *testing.T) {
	rng := utils.TestRng()
	store := blockstore.NewTestStore(t)
	lake, err := New(t.Context(), cdkdatalake.DefaultTestDataLakeConfig(t.TempDir()), store)
	require.NoError(t, err)
	defer func() { require.NoError(t, lake.Close()) }()

	writeRoads(t, rng, store, 5, 9)
	waitForCopies(t, lake, 9)
	requireCopies(t, lake, store, 5, 9)
}

func TestStartsAtTheLowestRetainedRoad(t *testing.T) {
	rng := utils.TestRng()
	store := blockstore.NewTestStore(t)
	writeRoads(t, rng, store, 1, 8)
	require.NoError(t, store.PruneBefore(5))

	lake, err := New(t.Context(), cdkdatalake.DefaultTestDataLakeConfig(t.TempDir()), store)
	require.NoError(t, err)
	defer func() { require.NoError(t, lake.Close()) }()

	waitForCopies(t, lake, 8)
	requireCopies(t, lake, store, 5, 8)
}

func TestResumesAfterTheHighestCopiedRoad(t *testing.T) {
	rng := utils.TestRng()
	store := blockstore.NewTestStore(t)
	config := cdkdatalake.DefaultTestDataLakeConfig(t.TempDir())
	writeRoads(t, rng, store, 1, 4)
	lake, err := New(t.Context(), config, store)
	require.NoError(t, err)
	waitForCopies(t, lake, 4)
	require.NoError(t, lake.Close())

	writeRoads(t, rng, store, 4, 8)
	lake, err = New(t.Context(), config, store)
	require.NoError(t, err)
	defer func() { require.NoError(t, lake.Close()) }()

	waitForCopies(t, lake, 8)
	requireCopies(t, lake, store, 1, 8)
}

func TestCopyingStopsAtARoadPrunedBeforeItWasCopied(t *testing.T) {
	rng := utils.TestRng()
	store := blockstore.NewTestStore(t)
	config := cdkdatalake.DefaultTestDataLakeConfig(t.TempDir())
	writeRoads(t, rng, store, 1, 4)
	lake, err := New(t.Context(), config, store)
	require.NoError(t, err)
	waitForCopies(t, lake, 4)
	require.NoError(t, lake.Close())

	writeRoads(t, rng, store, 4, 10)
	require.NoError(t, store.PruneBefore(7))
	for _, kind := range recordKinds {
		kindLake, err := cdkdatalake.NewCDKDataLake(t.Context(), kind.storeName, false, config)
		require.NoError(t, err)
		m := &mirror{kind: kind, store: store, lake: kindLake}
		require.ErrorIs(t, m.copyRecords(t.Context()), types.ErrPruned, kind.storeName)
		require.Equal(t, uint64(4), kindLake.GetDurableWatermark(), kind.storeName)
		require.NoError(t, kindLake.Close())
	}
}
