package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// blockHashEntry accumulates votes for a single block hash across epochs.
// votes holds every accepted vote (deduped by key across all epochs).
// epochWeight tracks the accumulated weight per epoch independently, so
// that quorum can be reached separately for each epoch's committee.
type blockHashEntry struct {
	votes       []*types.Signed[*types.LaneVote]
	epochWeight map[uint64]uint64
}

type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*blockHashEntry
}

func newBlockVotes() blockVotes {
	return blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*blockHashEntry{},
	}
}

// pushVote records vote for the given epoch where the voter is a member.
// Returns true the first time the epoch's accumulated weight reaches its LaneQuorum.
func (bv blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) bool {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return false
	}
	bv.byKey[k] = vote

	h := vote.Msg().Header().Hash()
	entry, ok := bv.byHash[h]
	if !ok {
		entry = &blockHashEntry{epochWeight: map[uint64]uint64{}}
		bv.byHash[h] = entry
	}
	entry.votes = append(entry.votes, vote)

	e, c := ep.EpochIndex, ep.Committee
	w := c.Weight(k)
	if w == 0 {
		return false
	}
	prev := entry.epochWeight[e]
	quorum := c.LaneQuorum()
	if prev >= quorum {
		return false
	}
	entry.epochWeight[e] = prev + w
	return prev+w >= quorum
}
