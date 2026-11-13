package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// blockVotes holds lane votes, their headers, and their QC for the applied epoch.
type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]
	qc     utils.Option[*types.LaneQC]
	// qcCounted is set once this block has formed a LaneQC.
	qcCounted bool
}

func newBlockVotes() *blockVotes {
	return &blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]{},
	}
}

// pushVote stores a vote verified against c and reports whether the signer is new.
func (bv *blockVotes) pushVote(c *types.Committee, vote *types.Signed[*types.LaneVote]) bool {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return false
	}
	bv.byKey[k] = vote
	meters.LaneVotesIngested.Add(1)
	bv.credit(c, vote)
	return true
}

// credit adds the vote's weight to its header bucket and, on reaching quorum,
// forms the LaneQC. A block is counted as a LaneQC only the first time it forms one.
func (bv *blockVotes) credit(c *types.Committee, vote *types.Signed[*types.LaneVote]) {
	if bv.qc.IsPresent() {
		return
	}
	k := vote.Key()
	w := c.Weight(k)
	if w == 0 {
		return
	}
	h := vote.Msg().Header().Hash()
	byHash, ok := bv.byHash[h]
	if !ok {
		byHash = &voteSet[*types.Signed[*types.LaneVote]]{}
		bv.byHash[h] = byHash
	}
	byHash.weight += w
	byHash.votes = append(byHash.votes, vote)
	if byHash.weight >= c.LaneQuorum() {
		bv.qc = utils.Some(types.NewLaneQC(byHash.votes))
		if !bv.qcCounted {
			bv.qcCounted = true
			meters.LaneQCs.Add(1)
		}
	}
}

func (bv *blockVotes) header(want types.BlockHeaderHash) utils.Option[*types.BlockHeader] {
	for _, vote := range bv.byKey {
		h := vote.Msg().Header()
		if h.Hash() == want {
			return utils.Some(h)
		}
	}
	return utils.None[*types.BlockHeader]()
}
