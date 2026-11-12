package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestConsensusSpecRestoredFromWALAndAnchor(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	anchor := data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}
	// Let f be epoch 0's first road. No anchor QC or WAL: spec f, with no CommitQC.
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{})
	require.NoError(t, err)
	require.Equal(t, types.ConsensusSpec{Epoch: anchor.Epochs[0], GenesisTimestamp: registry.GenesisTimestamp()}, i.consensusSpec.Load())

	// qc0 and qc1 certify f and f+1. Restoring both moves the spec to f+2.
	qc0 := types.BuildCommitQC(anchor.Epochs[0], keys, utils.None[*types.CommitQC](), nil)
	qc1 := types.BuildCommitQC(anchor.Epochs[0], keys, utils.Some(qc0), nil)
	i, err = newInner(anchor, registry.GenesisTimestamp(), &loadedState{commitQCs: []*types.CommitQC{qc0, qc1}})
	require.NoError(t, err)
	spec := i.consensusSpec.Load()
	require.Equal(t, utils.Some(qc1), spec.CommitQC)
	require.Equal(t, qc1.Index()+1, spec.Index())

	// Anchor at f drops qc0; the spec stays at f+2.
	anchor.First = utils.Some(data.AnchorFirst{CommitQC: qc0, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc0.View(), nil))})
	i.setAnchor(anchor)
	require.Equal(t, spec, i.consensusSpec.Load())

	// Anchor at f+1 drops all roads; the spec must remain unchanged.
	anchor.First = utils.Some(data.AnchorFirst{CommitQC: qc1, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc1.View(), nil))})
	i.setAnchor(anchor)
	require.Equal(t, spec, i.consensusSpec.Load())

	// With an empty WAL, restart recovers the same spec from the anchor.
	i, err = newInner(anchor, registry.GenesisTimestamp(), &loadedState{})
	require.NoError(t, err)
	require.Equal(t, spec, i.consensusSpec.Load())
}

func TestConsensusSpecAdvancesOnlyAfterPersistence(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	store := blockstore.NewTestStore(t)
	defer func() { require.NoError(t, store.Close()) }()
	ds, err := data.NewState(&data.Config{Registry: registry}, store)
	require.NoError(t, err)
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	defer func() { require.NoError(t, state.Close()) }()
	ep := ds.Anchor().Load().Epochs[0]
	// Let f be epoch 0's first road. qc0, qc1, qc2 certify f, f+1, f+2.
	qc0 := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), nil)
	qc1 := types.BuildCommitQC(ep, keys, utils.Some(qc0), nil)
	qc2 := types.BuildCommitQC(ep, keys, utils.Some(qc1), nil)
	spec := state.ConsensusSpec().Load()
	// Receiving all three QCs leaves the spec at f until persistence advances.
	for _, qc := range []*types.CommitQC{qc0, qc1, qc2} {
		require.NoError(t, state.PushCommitQC(t.Context(), qc))
		require.Equal(t, spec, state.ConsensusSpec().Load())
	}
	// Persisting qc0 then qc1 moves the spec to f+1 then f+2, still in epoch 0.
	// qc2 is not persisted, so the spec must not advance to f+3.
	for _, qc := range []*types.CommitQC{qc0, qc1} {
		state.markCommitQCsPersisted(qc)
		spec, err = state.ConsensusSpec().Wait(t.Context(), func(spec types.ConsensusSpec) bool { return spec.Index() == qc.Index()+1 })
		require.NoError(t, err)
		require.Equal(t, utils.Some(qc), spec.CommitQC)
		require.Equal(t, ep, spec.Epoch)
	}
	// A late qc0 completion must not rewind the spec.
	state.markCommitQCsPersisted(qc0)
	require.Equal(t, spec, state.ConsensusSpec().Load())
}
