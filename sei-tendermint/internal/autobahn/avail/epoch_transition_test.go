package avail

import (
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
)

type epochHarness struct {
	t        *testing.T
	rng      utils.Rng
	registry *epoch.Registry
	keys     []types.SecretKey
	state    *State
	stateDir string
}

func newEpochHarness(t *testing.T, n int, withDir bool) *epochHarness {
	t.Helper()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, n)
	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	dir := utils.None[string]()
	var stateDir string
	if withDir {
		stateDir = t.TempDir()
		dir = utils.Some(stateDir)
	}
	return &epochHarness{
		t: t, rng: rng, registry: registry, keys: keys, stateDir: stateDir,
		state: utils.OrPanic1(NewState(keys[0], ds, dir)),
	}
}

func (h *epochHarness) activate(weights map[types.PublicKey]uint64) *types.Epoch {
	h.t.Helper()
	ep, err := h.registry.ActivateEpoch(weights, types.OpenRoadRange(), time.Time{}, h.registry.FirstBlock())
	require.NoError(h.t, err)
	require.NoError(h.t, h.state.ApplyEpoch(ep))
	return ep
}

func (h *epochHarness) persistLane(key types.SecretKey, lane types.LaneID) string {
	h.t.Helper()
	signed := types.Sign(key, types.NewLaneProposal(
		types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(h.rng)),
	))
	require.NoError(h.t, h.state.persisters.blocks.MaybePruneAndPersistLane(
		lane,
		utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{key.Public(): 1})),
		utils.None[*types.CommitQC](),
		[]*types.Signed[*types.LaneProposal]{signed},
		utils.None[func(*types.Signed[*types.LaneProposal])](),
	))
	path := filepath.Join(h.stateDir, "blocks", lane.HexString())
	_, err := os.Stat(path)
	require.NoError(h.t, err)
	return path
}

func (h *epochHarness) pushTipcut(qc *types.CommitQC, resetWindow bool) {
	h.t.Helper()
	for inner, ctrl := range h.state.inner.Lock() {
		if resetWindow {
			idx := qc.Proposal().Index()
			inner.commitQCs = newQueue[types.RoadIndex, *types.CommitQC]()
			inner.commitQCs.first = idx
			inner.commitQCs.next = idx
		}
		inner.commitQCs.pushBack(qc)
		inner.latestCommitQC.Store(utils.Some(qc))
		ctrl.Updated()
	}
}

func TestApplyEpoch_AddsJoinerDefersLeaverUntilTipcutOmits(t *testing.T) {
	h := newEpochHarness(t, 3, true)
	a, b, cKey := h.keys[0], h.keys[1], types.GenSecretKey(h.rng)
	laneA := h.registry.LatestEpoch().Committee().Lane(a.Public()).OrPanic("a")
	laneB := h.registry.LatestEpoch().Committee().Lane(b.Public()).OrPanic("b")

	_, err := h.state.ProduceLocalBlock(laneA, 0, types.GenPayload(h.rng))
	require.NoError(t, err)
	_, err = h.state.ProduceLocalBlock(laneA, 1, types.GenPayload(h.rng))
	require.NoError(t, err)
	laneBPath := h.persistLane(b, laneB)

	ep0 := h.registry.LatestEpoch()
	ep := h.activate(map[types.PublicKey]uint64{a.Public(): 1, cKey.Public(): 1})
	laneC := ep.Committee().Lane(cKey.Public()).OrPanic("c")
	require.Equal(t, laneA, h.state.LocalLane().OrPanic("stay"))
	require.Equal(t, types.BlockNumber(2), h.state.NextBlock(laneA))
	require.Equal(t, types.BlockNumber(0), h.state.NextBlock(laneC))
	require.True(t, h.state.hasBlockLane(laneB))
	require.NoError(t, h.state.tryPruneLeaveLanes())
	require.True(t, h.state.hasBlockLane(laneB))

	h.pushTipcut(makeCommitQC(ep0, []types.SecretKey{a, b, h.keys[2]}, utils.None[*types.CommitQC](), nil, utils.None[*types.AppQC]()), false)
	require.NoError(t, h.state.tryPruneLeaveLanes())
	require.True(t, h.state.hasBlockLane(laneB))

	h.pushTipcut(makeCommitQC(ep, []types.SecretKey{a, cKey}, utils.None[*types.CommitQC](), nil, utils.None[*types.AppQC]()), true)
	require.NoError(t, h.state.tryPruneLeaveLanes())
	require.False(t, h.state.hasBlockLane(laneB))
	require.True(t, h.state.hasBlockLane(laneC))
	_, err = os.Stat(laneBPath)
	require.True(t, os.IsNotExist(err))
}

func TestTryPruneLeaveLanes_OrphanWALWithoutMaps(t *testing.T) {
	h := newEpochHarness(t, 3, true)
	a, b, cKey := h.keys[0], h.keys[1], types.GenSecretKey(h.rng)
	laneB := h.registry.LatestEpoch().Committee().Lane(b.Public()).OrPanic("b")
	laneBPath := h.persistLane(b, laneB)

	ep := h.activate(map[types.PublicKey]uint64{a.Public(): 1, cKey.Public(): 1})
	for inner := range h.state.inner.Lock() {
		delete(inner.blocks, laneB)
		delete(inner.votes, laneB)
		delete(inner.nextBlockToPersist, laneB)
		delete(inner.persistedBlockStart, laneB)
	}
	require.False(t, h.state.hasBlockLane(laneB))
	_, err := os.Stat(laneBPath)
	require.NoError(t, err)

	h.pushTipcut(makeCommitQC(ep, []types.SecretKey{a, cKey}, utils.None[*types.CommitQC](), nil, utils.None[*types.AppQC]()), false)
	require.NoError(t, h.state.tryPruneLeaveLanes())
	_, err = os.Stat(laneBPath)
	require.True(t, os.IsNotExist(err))
}

func TestSubscribeLaneProposals_LaneIdentity(t *testing.T) {
	t.Run("leave", func(t *testing.T) {
		h := newEpochHarness(t, 2, false)
		b := h.keys[1]
		sub, err := h.state.SubscribeLaneProposals(0)
		require.NoError(t, err)

		h.activate(map[types.PublicKey]uint64{b.Public(): 1})
		_, err = sub.Recv(t.Context())
		require.ErrorIs(t, err, ErrLaneIdentityChanged)
		_, err = h.state.SubscribeLaneProposals(0)
		require.ErrorIs(t, err, ErrBadLane)
	})

	t.Run("coalescedRejoin", func(t *testing.T) {
		h := newEpochHarness(t, 2, false)
		a, b := h.keys[0], h.keys[1]
		lane0 := h.state.LocalLane().OrPanic("genesis")
		sub, err := h.state.SubscribeLaneProposals(0)
		require.NoError(t, err)

		h.activate(map[types.PublicKey]uint64{b.Public(): 1})
		h.activate(map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1})

		lane1 := h.state.LocalLane().OrPanic("rejoin")
		require.NotEqual(t, lane0, lane1)
		require.Equal(t, types.BlockNumber(0), h.state.NextBlock(lane1))

		_, err = sub.Recv(t.Context())
		require.ErrorIs(t, err, ErrLaneIdentityChanged)

		sub2, err := h.state.SubscribeLaneProposals(0)
		require.NoError(t, err)
		require.Equal(t, lane1, sub2.lane)
	})
}
