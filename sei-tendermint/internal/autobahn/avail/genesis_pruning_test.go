package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestRecoveryAfterGenesisEpochsPruned(t *testing.T) {
	for _, withWAL := range []bool{false, true} {
		name := "anchor only"
		if withWAL {
			name = "anchor and commitQC WAL"
		}
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistryThrough(rng, 3, 3)
			proposal, blocks := types.ProposalAtBlocks(registry.MustEpoch(2), types.View{Height: registry.LastRoad(2)}, 1)
			prepare := types.BuildPrepareQC(keys, proposal)
			votes := make([]*types.Signed[*types.CommitVote], len(keys))
			for i, key := range keys {
				votes[i] = types.Sign(key, types.NewCommitVote(prepare))
			}
			qc := types.NewCommitQC(votes)
			full := types.NewFullCommitQC(qc, []*types.LaneBlockHeader{blocks[0].Header()})
			store := blockstore.NewTestStore(t)
			t.Cleanup(func() { _ = store.Close() })
			require.NoError(t, store.WriteBlock(types.NewRoadBlock(full, blocks)))
			app := types.NewAppProposal(qc.View(), types.GenAppHash(rng))
			require.NoError(t, store.WriteAppProposal(app))
			require.NoError(t, store.WriteAppQC(data.TestAppQC(keys, app)))
			require.NoError(t, store.Flush())
			registry.Store(registry.Epochs()[1:])
			for idx := range 2 {
				_, err := registry.Epoch(types.EpochIndex(idx))
				require.ErrorIs(t, err, types.ErrPruned)
			}
			ds, err := data.NewState(&data.Config{Registry: registry, Genesis: epoch.GenesisSchedule{Timestamp: testGenesisTimestamp}}, store)
			require.NoError(t, err)
			stateDir := utils.None[string]()
			wantTip := qc
			if withWAL {
				stateDir = utils.Some(t.TempDir())
				wantTip = types.BuildCommitQC(registry.MustEpoch(3), keys, utils.Some(qc), nil)
				cp, _, err := persist.NewCommitQCPersister(stateDir)
				require.NoError(t, err)
				require.NoError(t, cp.PruneAndPersist(wantTip.Height(), []*types.CommitQC{wantTip}))
				require.NoError(t, cp.Close())
			}
			state, err := NewState(keys[0], ds, stateDir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = state.Close() })
			spec := state.ConsensusSpec().Load()
			require.Equal(t, types.EpochIndex(3), spec.Epoch.EpochIndex())
			require.Equal(t, utils.Some(wantTip), spec.CommitQC)
			require.Equal(t, testGenesisTimestamp, spec.GenesisTimestamp)
		})
	}
}
