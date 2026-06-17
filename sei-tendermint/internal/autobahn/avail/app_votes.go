package avail

import (
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "avail")

type voteSet[V any] struct {
	weight uint64
	votes  []V
}

type appVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.AppVote]
	byHash map[types.Hash[*types.AppVote]]*voteSet[*types.Signed[*types.AppVote]]
}

func newAppVotes() appVotes {
	return appVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.AppVote]{},
		byHash: map[types.Hash[*types.AppVote]]*voteSet[*types.Signed[*types.AppVote]]{},
	}
}

// Returns qc if a new qc has been reached.
func (av appVotes) pushVote(c *types.Committee, vote *types.Signed[*types.AppVote]) (*types.AppQC, bool) {
	k := vote.Key()
	if _, ok := av.byKey[k]; ok {
		return nil, false
	}
	av.byKey[k] = vote
	byHash, ok := av.byHash[vote.Hash()]
	if !ok {
		if len(av.byHash) == 1 {
			logger.Error("appHash mismatch", slog.Uint64("n", uint64(vote.Msg().Proposal().GlobalNumber())))
		}
		byHash = &voteSet[*types.Signed[*types.AppVote]]{}
		av.byHash[vote.Hash()] = byHash
	}
	if byHash.weight >= c.AppQuorum() {
		return nil, false
	}
	byHash.weight += c.Weight(k)
	byHash.votes = append(byHash.votes, vote)
	if byHash.weight >= c.AppQuorum() {
		return types.NewAppQC(byHash.votes), true
	}
	return nil, false
}
