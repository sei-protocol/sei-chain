package avail

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func makeVoteEpoch(idx types.EpochIndex, weights map[types.PublicKey]uint64) *types.Epoch {
	c := utils.OrPanic1(types.NewCommittee(weights))
	first := types.RoadIndex(uint64(idx) * 108_000)
	rr := types.RoadRange{First: first, Next: first + 108_000}
	return types.NewEpoch(idx, rr, time.Time{}, c, 0)
}

func TestLaneVoteSet_Add(t *testing.T) {
	rng := utils.TestRng()
	lane := types.GenSecretKey(rng).Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	mkVote := func() *types.Signed[*types.LaneVote] {
		return types.Sign(types.GenSecretKey(rng), types.NewLaneVote(header))
	}

	set := &laneVoteSet{}
	require.False(t, set.add(1, 2, mkVote()).IsPresent())
	require.Equal(t, uint64(1), set.weight)
	require.Len(t, set.votes, 1)
	require.Nil(t, set.qc)

	qc, ok := set.add(1, 2, mkVote()).Get()
	require.True(t, ok)
	require.Equal(t, set.qc, qc)
	require.Equal(t, uint64(2), set.weight)
	require.Len(t, set.votes, 2)

	require.False(t, set.add(1, 2, mkVote()).IsPresent())
	require.Equal(t, uint64(2), set.weight)
	require.Len(t, set.votes, 2)

	heavy := &laneVoteSet{}
	require.True(t, heavy.add(3, 2, mkVote()).IsPresent())
	require.Equal(t, uint64(3), heavy.weight)
	require.Len(t, heavy.votes, 1)
	require.NotNil(t, heavy.qc)
}

func TestPushVote_ZeroWeightKeptForReweight(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyZ := types.GenSecretKey(rng)

	ep0 := makeVoteEpoch(0, map[types.PublicKey]uint64{keyA.Public(): 1})

	lane := keyA.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()

	bv := newBlockVotes()
	require.False(t, bv.pushVote(ep0, types.Sign(keyZ, types.NewLaneVote(header))).IsPresent())
	require.Contains(t, bv.byKey, keyZ.Public())
	set := bv.byHash[header.Hash()]
	require.NotNil(t, set)
	require.Equal(t, header, set.header)
	require.Equal(t, uint64(0), set.weight)
	require.Empty(t, set.votes)
}

func TestPushVote_CurrentOnly(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyE := types.GenSecretKey(rng) // next-only

	ep0 := makeVoteEpoch(0, map[types.PublicKey]uint64{keyA.Public(): 1})
	ep1 := makeVoteEpoch(1, map[types.PublicKey]uint64{keyE.Public(): 1})

	lane := keyA.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	h := header.Hash()

	bv := newBlockVotes()
	require.False(t, bv.pushVote(ep0, types.Sign(keyE, types.NewLaneVote(header))).IsPresent())
	require.Contains(t, bv.byKey, keyE.Public())
	require.Equal(t, uint64(0), bv.byHash[h].weight)

	qc, ok := bv.pushVote(ep0, types.Sign(keyA, types.NewLaneVote(header))).Get()
	require.True(t, ok)
	require.Equal(t, qc, bv.byHash[h].qc)

	got, ok := bv.laneQC().Get()
	require.True(t, ok)
	require.Equal(t, qc, got)

	// After advance, E is counted under ep1.
	require.True(t, bv.reweight(ep1))
	require.Equal(t, uint64(1), bv.byHash[h].weight)
	require.Len(t, bv.byHash[h].votes, 1)
	require.Equal(t, keyE.Public(), bv.byHash[h].votes[0].Key())
	require.NotNil(t, bv.byHash[h].qc)
	require.Equal(t, header, bv.byHash[h].header, "header preserved across reweight")
}

func TestPushVote_DedupsSigner(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyB := types.GenSecretKey(rng)
	keyC := types.GenSecretKey(rng)
	keyD := types.GenSecretKey(rng)
	weights := map[types.PublicKey]uint64{
		keyA.Public(): 1, keyB.Public(): 1, keyC.Public(): 1, keyD.Public(): 1,
	}
	ep := makeVoteEpoch(0, weights)

	lane := keyA.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()

	bv := newBlockVotes()
	vote := types.Sign(keyA, types.NewLaneVote(header))

	require.False(t, bv.pushVote(ep, vote).IsPresent(), "one of four validators is below quorum (2)")
	set := bv.byHash[header.Hash()]
	require.Equal(t, uint64(1), set.weight)

	require.False(t, bv.pushVote(ep, vote).IsPresent())
	require.Equal(t, uint64(1), set.weight, "duplicate vote must not double-count")
	require.Len(t, set.votes, 1)
}
