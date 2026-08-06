package avail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestApplyEpoch_AddsJoinerDefersLeaverUntilTipcutOmits(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	a, b := keys[0], keys[1]
	cKey := types.GenSecretKey(rng)

	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds, err := data.NewState(&data.Config{Registry: registry}, db)
	require.NoError(t, err)

	stateDir := t.TempDir()
	state, err := NewState(a, ds, utils.Some(stateDir))
	require.NoError(t, err)

	laneA := registry.LatestEpoch().Committee().Lane(a.Public()).OrPanic("a")
	laneB := registry.LatestEpoch().Committee().Lane(b.Public()).OrPanic("b")
	require.Equal(t, types.BlockNumber(0), state.NextBlock(laneA))
	require.Equal(t, types.BlockNumber(0), state.NextBlock(laneB))

	_, err = state.ProduceLocalBlock(laneA, 0, types.GenPayload(rng))
	require.NoError(t, err)
	_, err = state.ProduceLocalBlock(laneA, 1, types.GenPayload(rng))
	require.NoError(t, err)
	require.Equal(t, types.BlockNumber(2), state.NextBlock(laneA))

	// Persist one block for B so a WAL dir exists to delete later.
	signedB := types.Sign(b, types.NewLaneProposal(
		types.NewBlock(laneB, 0, types.BlockHeaderHash{}, types.GenPayload(rng)),
	))
	require.NoError(t, state.persisters.blocks.MaybePruneAndPersistLane(
		laneB,
		utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{b.Public(): 1})),
		utils.None[*types.CommitQC](),
		[]*types.Signed[*types.LaneProposal]{signedB},
		utils.None[func(*types.Signed[*types.LaneProposal])](),
	))
	laneBPath := filepath.Join(stateDir, "blocks", laneB.HexString())
	_, err = os.Stat(laneBPath)
	require.NoError(t, err)

	ep0 := registry.LatestEpoch()
	weights := map[types.PublicKey]uint64{
		a.Public():    1,
		cKey.Public(): 1,
	}
	ep, err := registry.ActivateEpoch(weights, types.OpenRoadRange(), time.Time{}, registry.FirstBlock())
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(1), ep.EpochIndex())
	require.Equal(t, types.NewLaneID(a.Public(), 0), ep.Committee().Lane(a.Public()).OrPanic("a"))
	require.Equal(t, types.NewLaneID(cKey.Public(), 1), ep.Committee().Lane(cKey.Public()).OrPanic("c"))

	require.NoError(t, state.ApplyEpoch(ep))

	laneA2 := ep.Committee().Lane(a.Public()).OrPanic("a")
	laneC := ep.Committee().Lane(cKey.Public()).OrPanic("c")
	require.Equal(t, laneA, laneA2)
	require.Equal(t, laneA2, state.LocalLane().OrPanic("stay"))
	require.Equal(t, types.BlockNumber(2), state.NextBlock(laneA2))
	require.Equal(t, types.BlockNumber(0), state.NextBlock(laneC))
	require.False(t, ep.Committee().HasLane(laneB))
	require.True(t, state.hasBlockLane(laneB))
	require.NoError(t, state.tryPruneLeaveLanes()) // no tipcut CommitQC yet
	require.True(t, state.hasBlockLane(laneB))

	// Tipcut still in leave epoch: committee names laneB → keep.
	qc0 := makeCommitQC(ep0, []types.SecretKey{a, b, keys[2]}, utils.None[*types.CommitQC](), nil, utils.None[*types.AppQC]())
	require.Equal(t, types.EpochIndex(0), qc0.Proposal().EpochIndex())
	require.True(t, ep0.Committee().HasLane(laneB))
	for inner, ctrl := range state.inner.Lock() {
		inner.commitQCs.pushBack(qc0)
		inner.latestCommitQC.Store(utils.Some(qc0))
		ctrl.Updated()
	}
	require.NoError(t, state.tryPruneLeaveLanes())
	require.True(t, state.hasBlockLane(laneB))

	// Tipcut advances to post-leave epoch: committee omits laneB → remove.
	qc1 := makeCommitQC(ep, []types.SecretKey{a, cKey}, utils.None[*types.CommitQC](), nil, utils.None[*types.AppQC]())
	require.Equal(t, types.EpochIndex(1), qc1.Proposal().EpochIndex())
	for inner, ctrl := range state.inner.Lock() {
		idx := qc1.Proposal().Index()
		inner.commitQCs = newQueue[types.RoadIndex, *types.CommitQC]()
		inner.commitQCs.first = idx
		inner.commitQCs.next = idx
		inner.commitQCs.pushBack(qc1)
		inner.latestCommitQC.Store(utils.Some(qc1))
		ctrl.Updated()
	}
	require.NoError(t, state.tryPruneLeaveLanes())
	require.False(t, state.hasBlockLane(laneB))
	require.True(t, state.hasBlockLane(laneC))
	_, err = os.Stat(laneBPath)
	require.True(t, os.IsNotExist(err))
}

// Orphan leave WAL (not in maps) is DeleteLane'd once tipcut omits the lane.
func TestTryPruneLeaveLanes_OrphanWALWithoutMaps(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	a, b := keys[0], keys[1]
	cKey := types.GenSecretKey(rng)

	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds, err := data.NewState(&data.Config{Registry: registry}, db)
	require.NoError(t, err)

	stateDir := t.TempDir()
	state, err := NewState(a, ds, utils.Some(stateDir))
	require.NoError(t, err)

	laneB := registry.LatestEpoch().Committee().Lane(b.Public()).OrPanic("b")
	signedB := types.Sign(b, types.NewLaneProposal(
		types.NewBlock(laneB, 0, types.BlockHeaderHash{}, types.GenPayload(rng)),
	))
	require.NoError(t, state.persisters.blocks.MaybePruneAndPersistLane(
		laneB,
		utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{b.Public(): 1})),
		utils.None[*types.CommitQC](),
		[]*types.Signed[*types.LaneProposal]{signedB},
		utils.None[func(*types.Signed[*types.LaneProposal])](),
	))
	laneBPath := filepath.Join(stateDir, "blocks", laneB.HexString())
	_, err = os.Stat(laneBPath)
	require.NoError(t, err)

	ep, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{a.Public(): 1, cKey.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	require.NoError(t, state.ApplyEpoch(ep))

	for inner := range state.inner.Lock() {
		delete(inner.blocks, laneB)
		delete(inner.votes, laneB)
		delete(inner.nextBlockToPersist, laneB)
		delete(inner.persistedBlockStart, laneB)
	}
	require.False(t, state.hasBlockLane(laneB))
	_, err = os.Stat(laneBPath)
	require.NoError(t, err)

	qc := makeCommitQC(ep, []types.SecretKey{a, cKey}, utils.None[*types.CommitQC](), nil, utils.None[*types.AppQC]())
	for inner, ctrl := range state.inner.Lock() {
		inner.commitQCs.pushBack(qc)
		inner.latestCommitQC.Store(utils.Some(qc))
		ctrl.Updated()
	}
	require.NoError(t, state.tryPruneLeaveLanes())
	_, err = os.Stat(laneBPath)
	require.True(t, os.IsNotExist(err))
}

func TestSubscribeLaneProposals_IdentityChange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]

	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds, err := data.NewState(&data.Config{Registry: registry}, db)
	require.NoError(t, err)
	state, err := NewState(a, ds, utils.None[string]())
	require.NoError(t, err)

	sub, err := state.SubscribeLaneProposals(0)
	require.NoError(t, err)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			_, err := sub.Recv(ctx)
			if !errors.Is(err, ErrLaneIdentityChanged) {
				return fmt.Errorf("Recv: %w", err)
			}
			return nil
		})
		s.Spawn(func() error {
			epLeave, err := registry.ActivateEpoch(
				map[types.PublicKey]uint64{b.Public(): 1},
				types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
			)
			if err != nil {
				return err
			}
			return state.ApplyEpoch(epLeave)
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, err = state.SubscribeLaneProposals(0)
	require.ErrorIs(t, err, ErrBadLane)
}

// Leave→rejoin under a new LaneID may coalesce to Some(new) without an observed
// None (AtomicSend keeps only the latest). Recv must end with ErrLaneIdentityChanged
// for the old streak, not panic; a fresh sub binds the new streak at block 0.
func TestSubscribeLaneProposals_CoalescedLeaveRejoin(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]

	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds, err := data.NewState(&data.Config{Registry: registry}, db)
	require.NoError(t, err)
	state, err := NewState(a, ds, utils.None[string]())
	require.NoError(t, err)

	lane0 := state.LocalLane().OrPanic("genesis")
	sub, err := state.SubscribeLaneProposals(0)
	require.NoError(t, err)

	epLeave, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	require.NoError(t, state.ApplyEpoch(epLeave))

	epRejoin, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	require.NoError(t, state.ApplyEpoch(epRejoin))

	lane1 := state.LocalLane().OrPanic("rejoin")
	require.NotEqual(t, lane0, lane1)
	require.Equal(t, types.EpochIndex(2), lane1.EJoin())
	require.Equal(t, types.BlockNumber(0), state.NextBlock(lane1))

	_, err = sub.Recv(t.Context())
	require.ErrorIs(t, err, ErrLaneIdentityChanged)

	sub2, err := state.SubscribeLaneProposals(0)
	require.NoError(t, err)
	require.Equal(t, lane1, sub2.lane)
}
