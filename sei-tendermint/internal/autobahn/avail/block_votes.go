package avail

import (
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

type blockVotes struct {
	byKey    map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHeader map[types.BlockHeaderHash][]*types.Signed[*types.LaneVote]
}

func newBlockVotes() blockVotes {
	return blockVotes{
		byKey:    map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHeader: map[types.BlockHeaderHash][]*types.Signed[*types.LaneVote]{},
	}
}

// Returns true iff a new QC has been constructed.
func (bv blockVotes) pushVote(c *types.Committee, vote *types.Signed[*types.LaneVote]) (*types.LaneQC, bool) {
	k := vote.Key()
	h := vote.Msg().Header().Hash()
	if _, ok := bv.byKey[k]; ok {
		return nil, false
	}
	bv.byKey[k] = vote
	bv.byHeader[h] = append(bv.byHeader[h], vote)
	if vs := bv.byHeader[h]; len(vs) == c.LaneQuorum() {
		return types.NewLaneQC(vs), true
	}
	return nil, false
}
