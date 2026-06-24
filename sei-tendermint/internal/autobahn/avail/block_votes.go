package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
)

// blockHashEntry accumulates votes for a single block hash across epochs.
// votes holds every accepted vote (deduped by key across all epochs).
// epochWeight tracks the accumulated weight per epoch independently, so
// that quorum can be reached separately for each epoch's committee.
type blockHashEntry struct {
	votes       []*types.Signed[*types.LaneVote]
	epochWeight map[epoch.Index]uint64
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

// pushVote records vote in every epoch where the voter is a member.
// Returns true the first time any epoch's accumulated weight reaches its LaneQuorum.
func (bv blockVotes) pushVote(window map[epoch.Index]*types.Committee, vote *types.Signed[*types.LaneVote]) bool {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return false
	}
	bv.byKey[k] = vote

	h := vote.Msg().Header().Hash()
	entry, ok := bv.byHash[h]
	if !ok {
		entry = &blockHashEntry{epochWeight: map[epoch.Index]uint64{}}
		bv.byHash[h] = entry
	}
	entry.votes = append(entry.votes, vote)

	quorumReached := false
	for e, c := range window {
		w := c.Weight(k)
		if w == 0 {
			continue
		}
		prev := entry.epochWeight[e]
		quorum := c.LaneQuorum()
		if prev >= quorum {
			continue
		}
		entry.epochWeight[e] = prev + w
		if prev+w >= quorum {
			quorumReached = true
		}
	}
	return quorumReached
}
