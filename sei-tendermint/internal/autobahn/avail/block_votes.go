package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// blockVotes holds lane votes, their headers, and their QC for one epoch.
type blockVotes struct {
	byKey  map[types.PublicKey]struct{}
	byHash map[types.LaneBlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]
	qc     utils.Option[*types.LaneQC]
}

func newBlockVotes() *blockVotes {
	return &blockVotes{
		byKey:  map[types.PublicKey]struct{}{},
		byHash: map[types.LaneBlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]{},
	}
}

// pushVote stores a vote verified against c and reports whether the signer is new.
func (bv *blockVotes) pushVote(c *types.Committee, vote *types.Signed[*types.LaneVote]) bool {
	k := vote.Key()
	w := c.Weight(k)
	if _, ok := bv.byKey[k]; ok || w == 0 {
		return false
	}
	// NOTE: we keep collecting votes even after qc is complete, because the finalized qc might
	// be different than our local one, and we need the finalized header to construct FullCommitQC.
	// NOTE: we remember QC for each height not, just the highest one, because a RoadBlock can include
	// only a bounded number of blocks per lane.
	bv.byKey[k] = struct{}{}
	meters.LaneVotesIngested.Add(1)
	h := vote.Msg().Header().Hash()
	byHash, ok := bv.byHash[h]
	if !ok {
		byHash = &voteSet[*types.Signed[*types.LaneVote]]{}
		bv.byHash[h] = byHash
	}
	byHash.weight += w
	byHash.votes = append(byHash.votes, vote)
	if !bv.qc.IsPresent() && byHash.weight >= c.LaneQuorum() {
		bv.qc = utils.Some(types.NewLaneQC(byHash.votes))
		meters.LaneQCs.Add(1)
	}
	return true
}

func (bv *blockVotes) header(want types.LaneBlockHeaderHash) utils.Option[*types.LaneBlockHeader] {
	if byHash, ok := bv.byHash[want]; ok {
		return utils.Some(byHash.votes[0].Msg().Header())
	}
	return utils.None[*types.LaneBlockHeader]()
}
