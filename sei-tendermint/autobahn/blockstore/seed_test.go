package blockstore

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// seedOf returns a seed of the road of block.
func seedOf(rng utils.Rng, block *types.RoadBlock) Seed {
	proposal := types.NewAppProposal(block.QC().QC().View(), types.GenAppHash(rng))
	return Seed{Block: block, AppQC: types.GenAppQCFor(rng, proposal)}
}

// seededConfig returns the config of the store in dir with seed.
func seededConfig(t *testing.T, dir string, seed Seed) *Config {
	t.Helper()
	cfg := persistenceConfig(t, dir)
	cfg.Seed = utils.Some(seed)
	return cfg
}

// seededRange returns the range of a store that holds only the records of road.
func seededRange(road types.RoadHeight) SuffixRange {
	return SuffixRange{First: road, NextBlock: road + 1, NextAppProposal: road + 1, NextAppQC: road + 1}
}

func TestNewWritesTheSeedToAnEmptyStore(t *testing.T) {
	rng := utils.TestRng()
	seed := seedOf(rng, storageBlocks(rng, 3, 1)[2])
	road := seed.Block.QC().Height()
	dir := t.TempDir()

	store := openPersistedStore(t, seededConfig(t, dir, seed))
	require.Equal(t, seededRange(road), status(t, store))
	// WaitForBlock returns a block only after a flush made it durable.
	block, err := store.WaitForBlock(t.Context(), road)
	require.NoError(t, err)
	requireStorageBlock(t, seed.Block, block)
	appQC, err := store.ReadAppQC(road)
	require.NoError(t, err)
	require.Equal(t, seed.AppQC.Proposal().AppHash(), appQC.OrPanic("seeded AppQC").Proposal().AppHash())
	require.NoError(t, store.Close())

	// A later open without the seed finds the same records.
	reopened := openPersistedStore(t, persistenceConfig(t, dir))
	defer func() { _ = reopened.Close() }()
	require.Equal(t, seededRange(road), status(t, reopened))
}

func TestNewCompletesTheSeedThatAnEarlierOpenWrote(t *testing.T) {
	for _, tc := range []struct {
		name    string
		written int
	}{
		{name: "the block", written: 1},
		{name: "the block and the AppProposal", written: 2},
		{name: "all three records", written: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			seed := seedOf(rng, storageBlocks(rng, 3, 1)[2])
			road := seed.Block.QC().Height()
			dir := t.TempDir()

			store := openPersistedStore(t, persistenceConfig(t, dir))
			writes := []func() error{
				func() error { return store.WriteBlock(seed.Block) },
				func() error { return store.WriteAppProposal(seed.AppQC.Proposal()) },
				func() error { return store.WriteAppQC(seed.AppQC) },
			}
			for _, write := range writes[:tc.written] {
				require.NoError(t, write())
			}
			require.NoError(t, store.Flush())
			require.NoError(t, store.Close())

			seeded := openPersistedStore(t, seededConfig(t, dir, seed))
			defer func() { _ = seeded.Close() }()
			require.Equal(t, seededRange(road), status(t, seeded))
		})
	}
}

func TestNewRefusesASeedForAStoreWithAnotherRoad(t *testing.T) {
	for _, tc := range []struct {
		name string
		// held are the roads whose blocks the store holds.
		held []types.RoadHeight
	}{
		{name: "an earlier road", held: []types.RoadHeight{1}},
		{name: "the road of the seed and a later road", held: []types.RoadHeight{2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			blocks := storageBlocks(rng, 4, 1)
			dir := t.TempDir()

			store := openPersistedStore(t, persistenceConfig(t, dir))
			for _, road := range tc.held {
				require.NoError(t, store.WriteBlock(blocks[road]))
			}
			before := status(t, store)
			require.NoError(t, store.Flush())
			require.NoError(t, store.Close())

			_, err := New(seededConfig(t, dir, seedOf(rng, blocks[2])))
			require.ErrorIs(t, err, types.ErrUnexpectedHeight)

			// The refused open wrote nothing.
			reopened := openPersistedStore(t, persistenceConfig(t, dir))
			defer func() { _ = reopened.Close() }()
			require.Equal(t, before, status(t, reopened))
		})
	}
}

func TestConfigRefusesASeedWhoseRecordsAreOfTwoRoads(t *testing.T) {
	rng := utils.TestRng()
	blocks := storageBlocks(rng, 2, 1)
	seed := seedOf(rng, blocks[0])
	seed.Block = blocks[1]

	require.ErrorIs(t, seededConfig(t, t.TempDir(), seed).Validate(), types.ErrUnexpectedHeight)
}
