package consensus

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func newTestBlockDB(t *testing.T, dir string) types.BlockStore {
	t.Helper()
	cfg := utils.OrPanic1(littblock.DefaultConfig(dir))
	db := utils.OrPanic1(blockstore.New(utils.OrPanic1(littblock.NewBlockDB(cfg))))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestDataState(registry *epoch.Registry) *data.State {
	store := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	return utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, store))
}

func newTestAvail(t *testing.T, registry *epoch.Registry, key types.SecretKey) (*data.State, *avail.State) {
	t.Helper()
	ds := newTestDataState(registry)
	av, err := avail.NewState(key, ds, utils.None[string]())
	require.NoError(t, err)
	t.Cleanup(func() { _ = av.Close() })
	return ds, av
}

// seedPersistedInner is a test helper that persists a persistedInner using the public API.
func seedPersistedInner(dir string, state *persistedInner) {
	p, _, err := persist.NewPersister[*pb.PersistedInner](utils.Some(dir), innerFile)
	if err != nil {
		panic(err)
	}
	if err := p.Persist(innerProtoConv.Encode(state)); err != nil {
		panic(err)
	}
}

// loadInner is a test helper that loads persisted data and creates inner.
// Mirrors what NewState does: avail first (aligned to the WAL tip via PushCommitQC),
// then newInner.
func loadInner(t *testing.T, dir string, registry *epoch.Registry, keys []types.SecretKey) (inner, error) {
	t.Helper()
	_, persisted, err := persist.NewPersister[*pb.PersistedInner](utils.Some(dir), innerFile)
	if err != nil {
		return inner{}, err
	}
	_, av := newTestAvail(t, registry, keys[0])
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() { _ = utils.IgnoreCancel(av.Run(ctx)) }()

	if p, ok := persisted.Get(); ok {
		decoded, err := innerProtoConv.Decode(p)
		if err != nil {
			return inner{}, err
		}
		if cqc, ok := decoded.CommitQC.Get(); ok {
			if err := alignAvailToTip(ctx, t, av, registry, keys, cqc); err != nil {
				return inner{}, err
			}
		}
	}
	return newInner(persisted, av.SubscribeConsensusSpec().Load())
}

// alignAvailToTip pushes CommitQCs 0..tip.Index() through avail and waits until
// the tip index is durable. The QCs are freshly built for the registry — the tip
// CommitQC used at restore comes from ConsensusSpec, not the WAL bytes.
// Callers must keep tip.Index() small — EpochLength boundaries are not replayed
// in unit tests.
func alignAvailToTip(
	ctx context.Context,
	t *testing.T,
	av *avail.State,
	registry *epoch.Registry,
	keys []types.SecretKey,
	tip *types.CommitQC,
) error {
	t.Helper()
	require.LessOrEqual(t, tip.Index(), types.RoadIndex(64), "alignAvailToTip: tip too high for unit replay")

	var prev utils.Option[*types.CommitQC]
	for idx := types.RoadIndex(0); idx <= tip.Index(); idx++ {
		ep, err := registry.EpochAt(idx)
		if err != nil {
			return err
		}
		qc := types.BuildCommitQC(ep, keys, prev, nil)
		if err := av.PushCommitQC(ctx, qc); err != nil {
			return err
		}
		prev = utils.Some(qc)
	}
	_, err := av.LastCommitQC().Wait(ctx, func(o utils.Option[*types.CommitQC]) bool {
		c, ok := o.Get()
		return ok && c.Index() >= tip.Index()
	})
	return err
}

// makePrepareQC creates a PrepareQC with valid signatures from the given keys.
func makePrepareQC(keys []types.SecretKey, proposal *types.Proposal) *types.PrepareQC {
	var votes []*types.Signed[*types.PrepareVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewPrepareVote(proposal)))
	}
	return types.NewPrepareQC(votes)
}

func TestNewInnerEmpty(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	_, av := newTestAvail(t, registry, keys[0])
	i, err := newInner(utils.None[*pb.PersistedInner](), av.SubscribeConsensusSpec().Load())
	require.NoError(t, err)
	require.False(t, i.PrepareVote.IsPresent(), "prepareVote should be None")
	require.False(t, i.CommitVote.IsPresent(), "commitVote should be None")
	require.False(t, i.TimeoutVote.IsPresent(), "timeoutVote should be None")
	require.Equal(t, types.EpochIndex(0), i.epoch.EpochIndex())
}

// TestNewInner_RejectsWALAheadOfSpec: after avail catch-up, ConsensusSpec must
// cover the WAL tip. A WAL tip ahead of the spec is a failed catch-up.
func TestNewInner_RejectsWALAheadOfSpec(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	registry.AdvanceIfNeeded(epoch.LastRoad(0))

	ep0, ok := registry.EpochByIndex(0)
	require.True(t, ok)
	ep1, ok := registry.EpochByIndex(1)
	require.True(t, ok)

	last := epoch.LastRoad(0)
	prev := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(ep0, types.View{Index: last - 1, Number: 0}, ep0.FirstBlock()))),
	})
	qcLast := types.BuildCommitQC(ep0, keys, utils.Some(prev), nil)
	require.Equal(t, last, qcLast.Index())

	// Spec tip still None (genesis-shaped stand-in for a withheld tip).
	view := types.View{Index: last + 1, Number: 0, EpochIndex: 1}
	proposal := types.GenProposalForEpoch(rng, ep1, view)
	vote := types.Sign(keys[0], types.NewPrepareVote(proposal))
	persisted := persistedInner{
		CommitQC:    utils.Some(qcLast),
		PrepareVote: utils.Some(vote),
	}

	genesis := types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: ep0}
	_, err := newInner(utils.Some(innerProtoConv.Encode(&persisted)), genesis)
	require.ErrorIs(t, err, ErrAvailBehindConsensus)
}

// TestNewInner_EqualTipKeepsVotes: matching tips take CommitQC from the spec and
// retain WAL votes for anti-equivocation.
func TestNewInner_EqualTipKeepsVotes(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	registry.AdvanceIfNeeded(epoch.LastRoad(0))

	ep0, ok := registry.EpochByIndex(0)
	require.True(t, ok)
	ep1, ok := registry.EpochByIndex(1)
	require.True(t, ok)

	last := epoch.LastRoad(0)
	prev := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(ep0, types.View{Index: last - 1, Number: 0}, ep0.FirstBlock()))),
	})
	qcLast := types.BuildCommitQC(ep0, keys, utils.Some(prev), nil)

	view := types.View{Index: last + 1, Number: 0, EpochIndex: 1}
	proposal := types.GenProposalForEpoch(rng, ep1, view)
	vote := types.Sign(keys[0], types.NewPrepareVote(proposal))
	persisted := persistedInner{
		CommitQC:    utils.Some(qcLast),
		PrepareVote: utils.Some(vote),
	}
	spec := types.ConsensusSpec{CommitQC: utils.Some(qcLast), Epoch: ep1}

	i, err := newInner(utils.Some(innerProtoConv.Encode(&persisted)), spec)
	require.NoError(t, err)
	require.Equal(t, last+1, i.View().Index)
	require.Equal(t, types.EpochIndex(1), i.epoch.EpochIndex())
	got, ok := i.PrepareVote.Get()
	require.True(t, ok)
	require.Equal(t, view, got.Msg().Proposal().View())
}

// TestRestore_BoundaryCatchUpSpecCoversWAL is the restart invariant blind-Spec
// trust depends on. After avail catch-up installs epoch 1 at the LastRoad(0)
// tip, ConsensusSpec must republish that tip so a WAL at the same tip restores
// without ErrAvailBehindConsensus and keeps anti-equivocation votes.
func TestRestore_BoundaryCatchUpSpecCoversWAL(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	registry.AdvanceIfNeeded(epoch.LastRoad(0))

	ds := newTestDataState(registry)
	av, err := avail.NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	t.Cleanup(func() { _ = av.Close() })

	last := epoch.LastRoad(0)
	var spec types.ConsensusSpec
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("data.Run", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		s.SpawnBgNamed("avail.Run", func() error {
			return utils.IgnoreCancel(av.Run(ctx))
		})
		if err := avail.DriveAdvance(ctx, av, keys, 1); err != nil {
			return fmt.Errorf("DriveAdvance: %w", err)
		}
		if _, err := av.LastCommitQC().Wait(ctx, func(o utils.Option[*types.CommitQC]) bool {
			c, ok := o.Get()
			return ok && c.Index() >= last
		}); err != nil {
			return fmt.Errorf("wait durable tip: %w", err)
		}
		got, err := av.SubscribeConsensusSpec().Wait(ctx, func(sp types.ConsensusSpec) bool {
			cqc, ok := sp.CommitQC.Get()
			return ok && cqc.Index() >= last && sp.Epoch.EpochIndex() >= 1
		})
		if err != nil {
			return fmt.Errorf("wait ConsensusSpec: %w", err)
		}
		spec = got
		return nil
	}))

	tip, ok := spec.CommitQC.Get()
	require.True(t, ok)
	require.Equal(t, last, tip.Index(), "catch-up must republish the boundary tip, not withhold")
	require.Equal(t, types.EpochIndex(1), spec.Epoch.EpochIndex())

	view := types.View{Index: last + 1, Number: 0, EpochIndex: 1}
	proposal := types.GenProposalForEpoch(rng, spec.Epoch, view)
	vote := types.Sign(keys[0], types.NewPrepareVote(proposal))
	persisted := persistedInner{
		CommitQC:    spec.CommitQC,
		PrepareVote: utils.Some(vote),
	}

	i, err := newInner(utils.Some(innerProtoConv.Encode(&persisted)), spec)
	require.NoError(t, err)
	require.Equal(t, last+1, i.View().Index)
	require.Equal(t, types.EpochIndex(1), i.epoch.EpochIndex())
	got, ok := i.PrepareVote.Get()
	require.True(t, ok, "equal-tip restore must keep anti-equivocation vote")
	require.Equal(t, view, got.Msg().Proposal().View())
}

func TestNewInnerPrepareVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create and persist a prepare vote at genesis view (0, 0)
	registry, keys := epoch.GenRegistry(rng, 1)
	key := keys[0]
	genesisProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 0, Number: 0})
	vote := types.Sign(key, types.NewPrepareVote(genesisProposal))

	seedPersistedInner(dir, &persistedInner{
		PrepareVote: utils.Some(vote),
	})

	// Load and verify
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	loaded, ok := i.PrepareVote.Get()
	require.True(t, ok, "prepareVote should be Some")
	require.NoError(t, utils.TestDiff(vote, loaded))
}

func TestNewInnerCommitVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create and persist a commit vote at genesis view (0, 0)
	registry, keys := epoch.GenRegistry(rng, 1)
	key := keys[0]
	genesisProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 0, Number: 0})
	prepareQC := makePrepareQC([]types.SecretKey{key}, genesisProposal)
	vote := types.Sign(key, types.NewCommitVote(genesisProposal))

	seedPersistedInner(dir, &persistedInner{
		PrepareQC:  utils.Some(prepareQC),
		CommitVote: utils.Some(vote),
	})

	// Load and verify
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	loaded, ok := i.CommitVote.Get()
	require.True(t, ok, "commitVote should be Some")
	require.NoError(t, utils.TestDiff(vote, loaded))
}

func TestNewInnerTimeoutVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create and persist a timeout vote at genesis view (0, 0)
	registry, keys := epoch.GenRegistry(rng, 1)
	key := keys[0]
	vote := types.NewFullTimeoutVote(key, types.View{Index: 0, Number: 0}, utils.None[*types.PrepareQC]())

	seedPersistedInner(dir, &persistedInner{
		TimeoutVote: utils.Some(vote),
	})

	// Load and verify
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	loaded, ok := i.TimeoutVote.Get()
	require.True(t, ok, "timeoutVote should be Some")
	require.NoError(t, utils.TestDiff(vote, loaded))
}

func TestNewInnerAllVotes(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create all vote types at genesis view (0, 0)
	registry, keys := epoch.GenRegistry(rng, 1)
	key := keys[0]
	genesisProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 0, Number: 0})
	prepareQC := makePrepareQC([]types.SecretKey{key}, genesisProposal)
	prepareVote := types.Sign(key, types.NewPrepareVote(genesisProposal))
	commitVote := types.Sign(key, types.NewCommitVote(genesisProposal))
	timeoutVote := types.NewFullTimeoutVote(key, types.View{Index: 0, Number: 0}, utils.None[*types.PrepareQC]())

	seedPersistedInner(dir, &persistedInner{
		PrepareQC:   utils.Some(prepareQC),
		PrepareVote: utils.Some(prepareVote),
		CommitVote:  utils.Some(commitVote),
		TimeoutVote: utils.Some(timeoutVote),
	})

	// Load and verify all
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareVote.IsPresent(), "prepareVote should be Some")
	require.True(t, i.CommitVote.IsPresent(), "commitVote should be Some")
	require.True(t, i.TimeoutVote.IsPresent(), "timeoutVote should be Some")
}

func TestNewInnerPartialState(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Only persist prepareVote
	registry, keys := epoch.GenRegistry(rng, 1)
	key := keys[0]
	genesisProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 0, Number: 0})
	prepareVote := types.Sign(key, types.NewPrepareVote(genesisProposal))

	seedPersistedInner(dir, &persistedInner{
		PrepareVote: utils.Some(prepareVote),
	})

	// Load - only prepareVote should be present
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareVote.IsPresent(), "prepareVote should be Some")
	require.False(t, i.CommitVote.IsPresent(), "commitVote should be None")
	require.False(t, i.TimeoutVote.IsPresent(), "timeoutVote should be None")
}

func TestNewInnerCommitQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create a CommitQC at index 5
	proposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	vote := types.NewCommitVote(proposal)
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	qc := types.NewCommitQC(votes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC: utils.Some(qc),
	})

	// Load and verify
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.CommitQC.IsPresent(), "CommitQC should be loaded")
	loadedQC, ok := i.CommitQC.Get()
	require.True(t, ok)
	require.Equal(t, types.RoadIndex(5), loadedQC.Proposal().Index())
	// View should be (6, 0) since CommitQC at index 5 advances to index 6
	require.Equal(t, types.View{Index: 6, Number: 0}, i.View())
}

func TestNewInnerTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create a CommitQC at index 5 (required for TimeoutQC at index 6)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at (6, 2) - this advances view to (6, 3)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load and verify
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.TimeoutQC.IsPresent(), "TimeoutQC should be loaded")
	// View should be (6, 3) since TimeoutQC at (6, 2) advances to (6, 3)
	require.Equal(t, types.View{Index: 6, Number: 3}, i.View())
}

func TestNewInnerTimeoutQCOnlyGenesis(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create TimeoutQC at (0, 2) - no CommitQC needed for index 0
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 0, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load and verify - should work without CommitQC since index is 0
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.TimeoutQC.IsPresent(), "TimeoutQC should be loaded")
	require.Equal(t, types.View{Index: 0, Number: 3}, i.View())
}

func TestNewInnerTimeoutQCWithoutCommitQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create TimeoutQC at index 6 WITHOUT CommitQC at index 5
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 0}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Should return error - TimeoutQC at index 6 requires CommitQC at index 5
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerTimeoutQCAheadOfCommitQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at index 10 (way ahead of CommitQC)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 10, Number: 0}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Should return error - TimeoutQC index must equal CommitQC.Index + 1
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerViewSpecStaleTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 10
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 10, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at index 5 (stale - behind CommitQC).
	// Since inner is persisted atomically, a mismatched index is always corrupt.
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 5, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load - stale TimeoutQC should be treated as corrupt state
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerViewSpecValidBothQCs(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at index 6, number 2 (valid - exactly CommitQC.Index + 1)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load - both should be present
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.CommitQC.IsPresent(), "CommitQC should be loaded")
	require.True(t, i.TimeoutQC.IsPresent(), "TimeoutQC should be loaded")
	// View should be (6, 3) - TimeoutQC at (6, 2) advances to (6, 3)
	require.Equal(t, types.View{Index: 6, Number: 3}, i.View())
}

func TestNewInnerStaleVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create stale vote at view (3, 0) - before current view (6, 0).
	// Since inner is persisted atomically, a mismatched view is corrupt.
	staleProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 3, Number: 0})
	staleVote := types.Sign(keys[0], types.NewPrepareVote(staleProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(staleVote),
	})

	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerFuturePrepareVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future vote at view (10, 0) - ahead of current view (6, 0)
	futureProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 10, Number: 0})
	futureVote := types.Sign(keys[0], types.NewPrepareVote(futureProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(futureVote),
	})

	// Should return error - future votes indicate corrupt state
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerFutureCommitVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future commit vote at view (10, 0)
	futureProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 10, Number: 0})
	futureVote := types.Sign(keys[0], types.NewCommitVote(futureProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:   utils.Some(commitQC),
		CommitVote: utils.Some(futureVote),
	})

	// Should return error
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerFutureTimeoutVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future timeout vote at view (10, 0)
	futureVote := types.NewFullTimeoutVote(keys[0], types.View{Index: 10, Number: 0}, utils.None[*types.PrepareQC]())

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		TimeoutVote: utils.Some(futureVote),
	})

	// Should return error
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCurrentViewVoteOk(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create vote at exactly current view (6, 0)
	currentProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 6, Number: 0})
	currentVote := types.Sign(keys[0], types.NewPrepareVote(currentProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(currentVote),
	})

	// Should succeed - current view votes are valid
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareVote.IsPresent(), "current view vote should be loaded")
}

func TestNewInnerTimeoutQCInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create valid CommitQC at index 5
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC signed by keys NOT in committee
	otherKeys := make([]types.SecretKey, 3)
	for i := range otherKeys {
		otherKeys[i] = types.GenSecretKey(rng)
	}
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range otherKeys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 0}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Should return error - invalid signatures on TimeoutQC
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCurrentViewVoteInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create valid CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create vote at current view (6, 0) but signed by key NOT in committee
	otherKey := types.GenSecretKey(rng)
	currentProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 6, Number: 0})
	badVote := types.Sign(otherKey, types.NewPrepareVote(currentProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(badVote),
	})

	// Should return error - current view votes must have valid signatures
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerStaleVoteInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create valid CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create stale vote at (3, 0) signed by key NOT in committee.
	// Since inner is persisted atomically, a mismatched view is corrupt.
	otherKey := types.GenSecretKey(rng)
	staleProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 3, Number: 0})
	badVote := types.Sign(otherKey, types.NewPrepareVote(staleProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(badVote),
	})

	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerPrepareQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create prepareQC at genesis view (0, 0)
	proposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 0, Number: 0})
	prepareQC := makePrepareQC(keys, proposal)

	seedPersistedInner(dir, &persistedInner{
		PrepareQC: utils.Some(prepareQC),
	})

	// Load and verify
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "prepareQC should be loaded")
}

func TestNewInnerStalePrepareQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create stale prepareQC at view (3, 0) - before current view (6, 0).
	// Since inner is persisted atomically, a mismatched view is corrupt.
	staleProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 3, Number: 0})
	stalePrepareQC := makePrepareQC(keys, staleProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(stalePrepareQC),
	})

	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCommitVoteWithoutPrepareQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Current view is (0, 0) (no CommitQC or TimeoutQC).
	// CommitVote requires PrepareQC justification.
	proposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 0, Number: 0})
	commitVote := types.Sign(keys[0], types.NewCommitVote(proposal))

	seedPersistedInner(dir, &persistedInner{
		CommitVote: utils.Some(commitVote),
	})

	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CommitVote present without PrepareQC")
}

func TestNewInnerFuturePrepareQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future prepareQC at index 10 (> current 6)
	futureProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 10, Number: 0})
	prepareQC := makePrepareQC(keys, futureProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Should return error - future prepareQC indicates corrupt state
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCurrentViewPrepareQCOk(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create prepareQC at current view (6, 0)
	currentProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(keys, currentProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Should succeed - current view prepareQC is valid
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "current view prepareQC should be loaded")
}

func TestNewInnerCurrentViewPrepareQCInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create prepareQC at current view (6, 0) but signed by keys NOT in committee
	otherKeys := make([]types.SecretKey, 3)
	for i := range otherKeys {
		otherKeys[i] = types.GenSecretKey(rng)
	}
	currentProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(otherKeys, currentProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Should return error - current view prepareQC has invalid signatures
	_, err := loadInner(t, dir, registry, keys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerPrepareQCIncludedInTimeoutVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)
	voteKey := keys[0]

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create prepareQC at current view (6, 0)
	currentProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(keys, currentProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Load state
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "prepareQC should be loaded")

	// Simulate what voteTimeout does: create a FullTimeoutVote using i.PrepareQC
	currentView := i.View()
	timeoutVote := types.NewFullTimeoutVote(voteKey, currentView, i.PrepareQC)

	// The timeoutVote should pass verification (which checks prepareQC is correctly included)
	err = timeoutVote.Verify(registry.LatestEpoch())
	require.NoError(t, err, "timeoutVote with loaded prepareQC should verify")

	// Verify the loaded prepareQC matches what we persisted
	loadedPrepareQC, ok := i.PrepareQC.Get()
	require.True(t, ok)
	require.Equal(t, currentProposal.View(), loadedPrepareQC.Proposal().View(),
		"loaded prepareQC should have the correct view")
}

// Test that pushTimeoutQC clears stale votes and prepareQC
func TestPushTimeoutQCClearsStaleState(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	registry, keys := epoch.GenRegistry(rng, 3)

	// Setup: Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Setup: Create prepareQC at current view (6, 0)
	currentProposal := types.GenProposalForEpoch(rng, registry.LatestEpoch(), types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(keys, currentProposal)

	// Setup: Create votes at current view (6, 0)
	prepareVote := types.Sign(keys[0], types.NewPrepareVote(currentProposal))
	commitVote := types.Sign(keys[0], types.NewCommitVote(currentProposal))
	timeoutVote := types.NewFullTimeoutVote(keys[0], types.View{Index: 6, Number: 0}, utils.Some(prepareQC))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareQC:   utils.Some(prepareQC),
		PrepareVote: utils.Some(prepareVote),
		CommitVote:  utils.Some(commitVote),
		TimeoutVote: utils.Some(timeoutVote),
	})

	// Load initial state and verify everything is present
	i, err := loadInner(t, dir, registry, keys)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "prepareQC should be loaded")
	require.True(t, i.PrepareVote.IsPresent(), "prepareVote should be loaded")
	require.True(t, i.CommitVote.IsPresent(), "commitVote should be loaded")
	require.True(t, i.TimeoutVote.IsPresent(), "timeoutVote should be loaded")
	require.Equal(t, types.View{Index: 6, Number: 0}, i.View(), "initial view should be (6, 0)")

	// Create a TimeoutQC for current view (6, 0) that advances to (6, 1)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 0}, utils.Some(prepareQC)))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	// Simulate pushTimeoutQC's Update callback
	newInner := inner{persistedInner: persistedInner{CommitQC: i.CommitQC, TimeoutQC: utils.Some(timeoutQC)}, epoch: i.epoch}

	// Verify: view advanced to (6, 1)
	require.Equal(t, types.View{Index: 6, Number: 1}, newInner.View(), "view should advance to (6, 1)")

	// Verify: prepareQC and all votes are cleared (they're for old view)
	require.False(t, newInner.PrepareQC.IsPresent(), "prepareQC should be cleared")
	require.False(t, newInner.PrepareVote.IsPresent(), "prepareVote should be cleared")
	require.False(t, newInner.CommitVote.IsPresent(), "commitVote should be cleared")
	require.False(t, newInner.TimeoutVote.IsPresent(), "timeoutVote should be cleared")
}

// failPersister is a Persister that always returns an error.
type failPersister[T protoutils.Message] struct{ err error }

func (f failPersister[T]) Persist(T) error { return f.err }

func TestRunOutputsPersistErrorPropagates(t *testing.T) {
	// Verify that a persist error in runOutputs propagates
	// and terminates the consensus component (instead of panicking).
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	db := newTestBlockDB(t, filepath.Join(dir, "blockdb"))
	ds, err := data.NewState(&data.Config{Registry: registry}, db)
	if err != nil {
		t.Fatalf("data.NewState: %v", err)
	}

	wantErr := errors.New("disk on fire")
	pers := utils.Some[persist.Persister[*pb.PersistedInner]](failPersister[*pb.PersistedInner]{err: wantErr})
	cs, err := newState(&Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.Some(dir),
	}, ds, pers, utils.None[*pb.PersistedInner]())
	require.NoError(t, err)

	// runOutputs should fail on the first Iter callback when it tries to persist.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = cs.runOutputs(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, wantErr)
}

func newConsensusState(t *testing.T, registry *epoch.Registry, key types.SecretKey) *State {
	t.Helper()
	s, err := NewState(&Config{
		Key:         key,
		ViewTimeout: func(types.View) time.Duration { return time.Hour },
	}, newTestDataState(registry))
	require.NoError(t, err)
	return s
}

func commitQCAtRoad(ep *types.Epoch, keys []types.SecretKey, idx types.RoadIndex) *types.CommitQC {
	parent := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(ep, types.View{Index: idx - 1, Number: 0}, ep.FirstBlock()))),
	})
	qc := types.BuildCommitQC(ep, keys, utils.Some(parent), nil)
	if qc.Proposal().Index() != idx {
		panic("commitQCAtRoad: BuildCommitQC landed on unexpected index")
	}
	return qc
}

func TestPushCommitQC_RotatesEpochAtBoundary(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	s := newConsensusState(t, registry, keys[0])
	require.Equal(t, types.EpochIndex(0), s.innerRecv.Load().epoch.EpochIndex())

	ep0, ok := registry.EpochByIndex(0)
	require.True(t, ok)
	qc := commitQCAtRoad(ep0, keys, epoch.LastRoad(0))
	require.Equal(t, epoch.LastRoad(0), qc.Proposal().Index())

	// Avail resolves the next-view epoch; pushSpecFromAvail installs it verbatim.
	ep1, err := registry.EpochAt(epoch.FirstRoad(1))
	require.NoError(t, err)
	require.NoError(t, s.pushSpecFromAvail(types.ConsensusSpec{CommitQC: utils.Some(qc), Epoch: ep1}))
	got := s.innerRecv.Load()
	require.Equal(t, types.EpochIndex(1), got.epoch.EpochIndex())
	require.Equal(t, epoch.FirstRoad(1), got.View().Index)
}

func TestNewState_ErrAvailBehindConsensus(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	dir := t.TempDir()

	ep0, ok := registry.EpochByIndex(0)
	require.True(t, ok)
	qc := commitQCAtRoad(ep0, keys, 3)
	seedPersistedInner(dir, &persistedInner{CommitQC: utils.Some(qc)})

	_, err := NewState(&Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.Some(dir),
	}, newTestDataState(registry))
	require.ErrorIs(t, err, ErrAvailBehindConsensus)
}
