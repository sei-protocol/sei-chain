package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPersistedCommitQCFromCursor(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	anchor := data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{})
	require.NoError(t, err)
	require.False(t, i.persistedCommitQC().IsPresent())

	qc0 := types.BuildCommitQC(anchor.Epochs[0], keys, utils.None[*types.CommitQC](), nil)
	qc1 := types.BuildCommitQC(anchor.Epochs[0], keys, utils.Some(qc0), nil)
	i, err = newInner(anchor, registry.GenesisTimestamp(), &loadedState{commitQCs: []*types.CommitQC{qc0, qc1}})
	require.NoError(t, err)
	require.Equal(t, utils.Some(qc1), i.persistedCommitQC())

	anchor.First = utils.Some(data.AnchorFirst{CommitQC: qc0, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc0.View(), nil))})
	i.prune(anchor)
	require.Equal(t, utils.Some(qc1), i.persistedCommitQC())

	anchor.First = utils.Some(data.AnchorFirst{CommitQC: qc1, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc1.View(), nil))})
	i.prune(anchor)
	require.Equal(t, uint64(0), i.roads.Len())
	require.Equal(t, qc1.Index()+1, i.nextCommitQCToPersist)
	require.Equal(t, utils.Some(qc1), i.persistedCommitQC())

	i, err = newInner(anchor, registry.GenesisTimestamp(), &loadedState{})
	require.NoError(t, err)
	require.Equal(t, utils.Some(qc1), i.persistedCommitQC())
}

func TestCommitQCPersistenceCursor(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	store, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ds, err := data.NewState(&data.Config{Registry: registry}, store)
	require.NoError(t, err)
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, state.Close()) })

	ep := ds.Anchor().Load().Epochs[0]
	qc0 := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), nil)
	qc1 := types.BuildCommitQC(ep, keys, utils.Some(qc0), nil)
	require.NoError(t, state.PushCommitQC(t.Context(), qc0))
	require.False(t, state.LastCommitQC().IsPresent())
	batch, err := state.collectPersistBatch(t.Context())
	require.NoError(t, err)
	require.Equal(t, []*types.CommitQC{qc0}, batch.commitQCs.tail)

	state.markCommitQCsPersisted(qc0)
	spec, err := state.SubscribeConsensusSpec().Wait(t.Context(), func(spec types.ConsensusSpec) bool {
		return spec.CommitQC.IsPresent() && spec.Index() == qc0.Index()+1
	})
	require.NoError(t, err)
	require.Equal(t, utils.Some(qc0), spec.CommitQC)
	require.NoError(t, state.PushCommitQC(t.Context(), qc1))
	batch, err = state.collectPersistBatch(t.Context())
	require.NoError(t, err)
	require.Equal(t, []*types.CommitQC{qc1}, batch.commitQCs.tail)
	state.markCommitQCsPersisted(qc1)
	state.markCommitQCsPersisted(qc0)
	require.Equal(t, utils.Some(qc1), state.LastCommitQC())
}
