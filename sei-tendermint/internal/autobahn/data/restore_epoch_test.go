package data

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// restoredRegistry opens a registry with the genesis of source from a snapshot restored from
// source's epoch_registry row, as a state sync restore leaves it.
func restoredRegistry(t *testing.T, source *epoch.Registry, genesis *types.Epoch) *epoch.Registry {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, epoch.RestoreSnapshot(dir, utils.Some(source.MarshalSnapshot())))
	return utils.OrPanic1(epoch.NewRegistry(genesis.Committee(), genesis.RoadRange().First, time.Time{}, utils.Some(dir)))
}

func TestRestoredStateAnchorsOnTheEpochOfTheRestoredRoad(t *testing.T) {
	rng := utils.TestRng()
	const x = types.EpochIndex(3)
	source, keys := epoch.GenRegistryThrough(rng, 3, x+1)
	genesis := source.MustEpoch(0)
	require.NoError(t, source.PruneBefore(x-1))
	registry := restoredRegistry(t, source, genesis)

	h := registry.LastRoad(x-1) + 1
	qc, blocks := commitQCAtRoad(utils.OrPanic1(registry.EpochAt(h)), keys, h)
	store := newTestStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	state := newTestState(t, &Config{Registry: registry}, store)

	anchor := state.Anchor().Load()
	require.True(t, epoch.EpochEqual(source.MustEpoch(x), anchor.Epochs[0]))
	require.True(t, epoch.EpochEqual(source.MustEpoch(x+1), anchor.Epochs[1]))
}
