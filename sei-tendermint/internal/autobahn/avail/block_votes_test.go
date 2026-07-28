package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func makeVoteEpoch(idx types.EpochIndex, weights map[types.PublicKey]uint64) *types.Epoch {
	c := utils.OrPanic1(types.NewCommittee(weights))
	first := types.RoadIndex(uint64(idx) * 108_000)
	rr := types.RoadRange{First: first, Next: first + 108_000}
	return types.NewEpoch(idx, rr, c)
}

func TestLaneVoteSet_Add(t *testing.T) {
	rng := utils.TestRng()
	lane := types.GenSecretKey(rng).Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	mkVote := func() *types.Signed[*types.LaneVote] {
		return types.Sign(types.GenSecretKey(rng), types.NewLaneVote(header))
	}

	set := &laneVoteSet{}
	require.False(t, set.add(1, 2, mkVote()))
	require.Equal(t, uint64(1), set.weight)
	require.Len(t, set.votes, 1)

	require.True(t, set.add(1, 2, mkVote()))
	require.Equal(t, uint64(2), set.weight)
	require.Len(t, set.votes, 2)

	require.True(t, set.add(1, 2, mkVote()))
	require.Equal(t, uint64(3), set.weight)
	require.Len(t, set.votes, 3)

	heavy := &laneVoteSet{}
	require.True(t, heavy.add(3, 2, mkVote()))
	require.Equal(t, uint64(3), heavy.weight)
	require.Len(t, heavy.votes, 1)
}

func TestPushVote_ZeroWeightNotRetained(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyZ := types.GenSecretKey(rng)

	ep0 := makeVoteEpoch(0, map[types.PublicKey]uint64{keyA.Public(): 1})

	lane := keyA.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()

	bv := newBlockVotes()
	require.False(t, bv.pushVote(ep0, types.Sign(keyZ, types.NewLaneVote(header))).IsPresent())
	require.NotContains(t, bv.byKey, keyZ.Public())
	require.Empty(t, bv.byHash)
}

func TestPushVote_CurrentCommitteeOnly(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyB := types.GenSecretKey(rng)
	keyC := types.GenSecretKey(rng)
	keyD := types.GenSecretKey(rng)
	keyE := types.GenSecretKey(rng)

	// 4×1 → Faulty=1, LaneQuorum=2.
	ep0 := makeVoteEpoch(0, map[types.PublicKey]uint64{
		keyA.Public(): 1, keyB.Public(): 1, keyC.Public(): 1, keyD.Public(): 1,
	})
	ep1 := makeVoteEpoch(1, map[types.PublicKey]uint64{
		keyA.Public(): 1, keyB.Public(): 1, keyC.Public(): 1, keyD.Public(): 1, keyE.Public(): 1,
	})

	lane := keyA.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	h := header.Hash()

	bv := newBlockVotes()
	require.False(t, bv.pushVote(ep0, types.Sign(keyE, types.NewLaneVote(header))).IsPresent())
	require.NotContains(t, bv.byKey, keyE.Public())

	require.False(t, bv.pushVote(ep0, types.Sign(keyA, types.NewLaneVote(header))).IsPresent())
	qc, ok := bv.pushVote(ep0, types.Sign(keyB, types.NewLaneVote(header))).Get()
	require.True(t, ok)
	gotQC, ok := bv.qc.Get()
	require.True(t, ok)
	require.Equal(t, qc, gotQC)

	// ep1: Faulty=(5-1)/3=1, LaneQuorum=2; A+B still form quorum under new weights.
	bv.reweight(ep1)
	require.True(t, bv.qc.IsPresent())
	require.False(t, bv.pushVote(ep1, types.Sign(keyE, types.NewLaneVote(header))).IsPresent())
	require.Contains(t, bv.byKey, keyE.Public())
	require.Equal(t, header, bv.byHash[h].header)
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

func TestPushVote_ReweightAfterAdvance(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyB := types.GenSecretKey(rng)
	keyC := types.GenSecretKey(rng)
	keyD := types.GenSecretKey(rng)

	// 4×1 → LaneQuorum=2.
	ep0 := makeVoteEpoch(0, map[types.PublicKey]uint64{
		keyA.Public(): 1, keyB.Public(): 1, keyC.Public(): 1, keyD.Public(): 1,
	})
	// Only A remains → Faulty=0, LaneQuorum=1; A alone forms QC under new Current.
	ep1 := makeVoteEpoch(1, map[types.PublicKey]uint64{keyA.Public(): 1})

	lane := keyA.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	h := header.Hash()

	bv := newBlockVotes()
	require.False(t, bv.pushVote(ep0, types.Sign(keyA, types.NewLaneVote(header))).IsPresent())
	require.True(t, bv.pushVote(ep0, types.Sign(keyB, types.NewLaneVote(header))).IsPresent())

	bv.reweight(ep1)
	require.Equal(t, uint64(1), bv.byHash[h].weight)
	require.Len(t, bv.byHash[h].votes, 1)
	require.Equal(t, keyA.Public(), bv.byHash[h].votes[0].Key())
	require.True(t, bv.qc.IsPresent())
	require.NotContains(t, bv.byKey, keyB.Public(), "zero-weight signer removed from byKey")
}

func TestPushVote_OneQCPerBlockVotes(t *testing.T) {
	rng := utils.TestRng()
	keyA := types.GenSecretKey(rng)
	keyB := types.GenSecretKey(rng)
	keyC := types.GenSecretKey(rng)
	keyD := types.GenSecretKey(rng)

	// 4×1 → LaneQuorum=2.
	ep := makeVoteEpoch(0, map[types.PublicKey]uint64{
		keyA.Public(): 1, keyB.Public(): 1, keyC.Public(): 1, keyD.Public(): 1,
	})

	lane := keyA.Public()
	header1 := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	header2 := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	require.NotEqual(t, header1.Hash(), header2.Hash())

	bv := newBlockVotes()
	require.False(t, bv.pushVote(ep, types.Sign(keyA, types.NewLaneVote(header1))).IsPresent())
	require.True(t, bv.pushVote(ep, types.Sign(keyB, types.NewLaneVote(header1))).IsPresent())
	require.True(t, bv.qc.IsPresent())
	got, ok := bv.qc.Get()
	require.True(t, ok)
	require.Equal(t, header1.Hash(), got.Header().Hash())

	// Competing hash is indexed in byHash (headers() reconstruction) and byKey
	// (reweight), but must not replace the single LaneQC.
	require.False(t, bv.pushVote(ep, types.Sign(keyC, types.NewLaneVote(header2))).IsPresent())
	require.False(t, bv.pushVote(ep, types.Sign(keyD, types.NewLaneVote(header2))).IsPresent())
	require.Contains(t, bv.byKey, keyC.Public())
	require.Contains(t, bv.byKey, keyD.Public())
	require.Contains(t, bv.byHash, header2.Hash())
	require.Equal(t, header2, bv.byHash[header2.Hash()].header)
	require.Empty(t, bv.byHash[header2.Hash()].votes, "post-QC competing votes are not credited toward a second QC")
	got, ok = bv.qc.Get()
	require.True(t, ok)
	require.Equal(t, header1.Hash(), got.Header().Hash())
}
