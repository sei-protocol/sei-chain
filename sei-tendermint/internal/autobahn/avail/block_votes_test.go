package avail

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestBlockVotes_Reweight(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	c := types.GenSecretKey(rng)
	d := types.GenSecretKey(rng)
	mk := func(weights map[types.PublicKey]uint64, idx types.EpochIndex, first, next types.RoadIndex) *types.Epoch {
		return types.NewEpoch(idx, types.RoadRange{First: first, Next: next}, time.Time{},
			utils.OrPanic1(types.NewCommittee(weights)), 0)
	}
	headerOn := func(ep *types.Epoch, sk types.SecretKey) *types.BlockHeader {
		lane := ep.Committee().Lane(sk.Public()).OrPanic("lane")
		return types.NewBlock(lane, 0, types.BlockHeaderHash{}, &types.Payload{}).Header()
	}

	t.Run("stay still has quorum", func(t *testing.T) {
		ep0 := mk(map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1, c.Public(): 1, d.Public(): 1}, 0, 0, 10)
		h := headerOn(ep0, a)
		vote := func(sk types.SecretKey) *types.Signed[*types.LaneVote] {
			return types.Sign(sk, types.NewLaneVote(h))
		}
		bv := newBlockVotes()
		require.True(t, bv.pushVote(ep0, vote(a)))
		require.False(t, bv.qc.IsPresent())
		require.True(t, bv.pushVote(ep0, vote(b)))
		require.True(t, bv.qc.IsPresent())
		require.True(t, bv.pushVote(ep0, vote(d)))

		ep1 := types.NewEpoch(1, types.RoadRange{First: 10, Next: 20}, time.Time{},
			utils.OrPanic1(ep0.Committee().DeriveNext(map[types.PublicKey]uint64{
				a.Public(): 1, b.Public(): 1, c.Public(): 1,
			}, 1)), 0)
		bv.reweight(ep1)
		qc, ok := bv.qc.Get()
		require.True(t, ok)
		require.Equal(t, h.Hash(), qc.Header().Hash())
		require.Equal(t, 3, len(bv.byKey))
		require.True(t, bv.header(h.Hash()).IsPresent())
	})

	t.Run("leaver not credited", func(t *testing.T) {
		ep0 := mk(map[types.PublicKey]uint64{
			a.Public(): 1, d.Public(): 1,
			types.GenSecretKey(rng).Public(): 1, types.GenSecretKey(rng).Public(): 1,
		}, 0, 0, 10)
		h := headerOn(ep0, a)
		bv := newBlockVotes()
		bv.pushVote(ep0, types.Sign(d, types.NewLaneVote(h)))
		ep1 := types.NewEpoch(1, types.RoadRange{First: 10, Next: 20}, time.Time{},
			utils.OrPanic1(ep0.Committee().DeriveNext(map[types.PublicKey]uint64{a.Public(): 1}, 1)), 0)
		bv.reweight(ep1)
		require.False(t, bv.qc.IsPresent())
		require.Equal(t, 1, len(bv.byKey))
	})

	t.Run("weight cut drops QC", func(t *testing.T) {
		ep0 := mk(map[types.PublicKey]uint64{a.Public(): 3, b.Public(): 1, c.Public(): 1, d.Public(): 1}, 0, 0, 10)
		require.Equal(t, uint64(2), ep0.Committee().LaneQuorum())
		h := headerOn(ep0, a)
		bv := newBlockVotes()
		require.True(t, bv.pushVote(ep0, types.Sign(a, types.NewLaneVote(h))))
		require.True(t, bv.qc.IsPresent())
		ep1 := types.NewEpoch(1, types.RoadRange{First: 10, Next: 20}, time.Time{},
			utils.OrPanic1(ep0.Committee().DeriveNext(map[types.PublicKey]uint64{
				a.Public(): 1, b.Public(): 1, c.Public(): 5, d.Public(): 5,
			}, 1)), 0)
		require.Equal(t, uint64(4), ep1.Committee().LaneQuorum())
		bv.reweight(ep1)
		require.False(t, bv.qc.IsPresent())
		require.True(t, bv.header(h.Hash()).IsPresent())
	})

	t.Run("weight bump forms QC", func(t *testing.T) {
		ep0 := mk(map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1, c.Public(): 5, d.Public(): 5}, 0, 0, 10)
		require.Equal(t, uint64(4), ep0.Committee().LaneQuorum())
		h := headerOn(ep0, a)
		vote := func(sk types.SecretKey) *types.Signed[*types.LaneVote] {
			return types.Sign(sk, types.NewLaneVote(h))
		}
		bv := newBlockVotes()
		require.True(t, bv.pushVote(ep0, vote(a)))
		require.True(t, bv.pushVote(ep0, vote(b)))
		require.False(t, bv.qc.IsPresent())
		ep1 := types.NewEpoch(1, types.RoadRange{First: 10, Next: 20}, time.Time{},
			utils.OrPanic1(ep0.Committee().DeriveNext(map[types.PublicKey]uint64{
				a.Public(): 5, b.Public(): 5, c.Public(): 1, d.Public(): 1,
			}, 1)), 0)
		require.Equal(t, uint64(4), ep1.Committee().LaneQuorum())
		bv.reweight(ep1)
		qc, ok := bv.qc.Get()
		require.True(t, ok)
		require.Equal(t, h.Hash(), qc.Header().Hash())
	})
}
