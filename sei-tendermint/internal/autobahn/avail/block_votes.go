package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]
}

func newBlockVotes() blockVotes {
	return blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]{},
	}
}

// Returns true iff a new QC has been constructed.
// Weight is counted under ep's committee (PushVote passes the applied epoch).
// TODO: votes accepted only under the Anchor epoch can have Weight 0 under the
// applied committee but are still appended into the LaneQC sig list; filter
// those out when forming a LaneQC (leave-across-epochs).
func (bv blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) (*types.LaneQC, bool) {
	c := ep.Committee()
	k := vote.Key()
	h := vote.Msg().Header().Hash()
	if _, ok := bv.byKey[k]; ok {
		return nil, false
	}
	bv.byKey[k] = vote
	byHash, ok := bv.byHash[h]
	if !ok {
		byHash = &voteSet[*types.Signed[*types.LaneVote]]{}
		bv.byHash[h] = byHash
	}
	if byHash.weight >= c.LaneQuorum() {
		return nil, false
	}
	byHash.weight += c.Weight(k)
	byHash.votes = append(byHash.votes, vote)
	if byHash.weight >= c.LaneQuorum() {
		return types.NewLaneQC(byHash.votes), true
	}
	return nil, false
}
