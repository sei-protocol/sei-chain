package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LaneVotes have two jobs: byKey keeps headers for FullCommitQC; byHash
// accumulates applied-epoch weight for LaneQC. byKey is retained even after the
// latest CommitQC moves into a new epoch, since a FullCommitQC in the previous
// epoch may still need those headers — so we can't clear it on epoch change.
// qc is set once weight reaches quorum under the applied committee and cleared
// on reweight.
type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]
	qc     utils.Option[*types.LaneQC]
}

func newBlockVotes() *blockVotes {
	return &blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]{},
	}
}

// return true iff the vote is newly stored in byKey.
func (bv *blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) bool {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return false
	}
	bv.byKey[k] = vote
	bv.credit(ep, vote)
	return true
}

func (bv *blockVotes) reweight(ep *types.Epoch) {
	bv.qc = utils.None[*types.LaneQC]()
	clear(bv.byHash)
	for _, vote := range bv.byKey {
		bv.credit(ep, vote)
	}
}

func (bv *blockVotes) credit(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) {
	if bv.qc.IsPresent() {
		return
	}
	c := ep.Committee()
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
