package avail

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestBlockVotes_RecountStayFormsLaneQC(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	c := types.GenSecretKey(rng)
	d := types.GenSecretKey(rng)

	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: 10}, time.Time{},
		utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
			a.Public(): 1, b.Public(): 1, c.Public(): 1, d.Public(): 1,
		})), 0)
	lane := ep0.Committee().Lane(a.Public()).OrPanic("lane")
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, &types.Payload{}).Header()
	vote := func(sk types.SecretKey) *types.Signed[*types.LaneVote] {
		return types.Sign(sk, types.NewLaneVote(header))
	}

	bv := newBlockVotes()
	require.True(t, bv.pushVote(ep0, vote(a)))
	require.False(t, bv.qc.IsPresent())
	require.True(t, bv.pushVote(ep0, vote(b)))
	qc, ok := bv.qc.Get()
	require.True(t, ok)
	require.Equal(t, header.Hash(), qc.Header().Hash())
	require.True(t, bv.pushVote(ep0, vote(d)))

	require.True(t, bv.header(header.Hash()).IsPresent())
	require.Equal(t, 3, len(bv.byKey))

	ep1 := types.NewEpoch(1, types.RoadRange{First: 10, Next: 20}, time.Time{},
		utils.OrPanic1(ep0.Committee().DeriveNext(map[types.PublicKey]uint64{
			a.Public(): 1, b.Public(): 1, c.Public(): 1,
		}, 1)), 0)
	bv.reweight(ep1)

	qc, ok = bv.qc.Get()
	require.True(t, ok)
	require.Equal(t, header.Hash(), qc.Header().Hash())
	require.Equal(t, 3, len(bv.byKey))
	require.True(t, bv.header(header.Hash()).IsPresent())
}

func TestBlockVotes_ZeroWeightNotCreditedUnderApplied(t *testing.T) {
	rng := utils.TestRng()
	stay := types.GenSecretKey(rng)
	leaver := types.GenSecretKey(rng)
	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: 10}, time.Time{},
		utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
			stay.Public(): 1, leaver.Public(): 1,
			types.GenSecretKey(rng).Public(): 1, types.GenSecretKey(rng).Public(): 1,
		})), 0)
	lane := ep0.Committee().Lane(stay.Public()).OrPanic("lane")
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, &types.Payload{}).Header()

	bv := newBlockVotes()
	bv.pushVote(ep0, types.Sign(leaver, types.NewLaneVote(header)))

	ep1 := types.NewEpoch(1, types.RoadRange{First: 10, Next: 20}, time.Time{},
		utils.OrPanic1(ep0.Committee().DeriveNext(map[types.PublicKey]uint64{
			stay.Public(): 1,
		}, 1)), 0)
	bv.reweight(ep1)
	require.False(t, bv.qc.IsPresent())
	require.Equal(t, 1, len(bv.byKey))
}
