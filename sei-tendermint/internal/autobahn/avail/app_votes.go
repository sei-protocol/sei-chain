package avail

import (
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "avail")

type voteSet[V any] struct {
	weight uint64
	votes  []V
}

type road struct {
	epoch     *types.Epoch
	commitQC  *types.CommitQC
	appByKey  map[types.PublicKey]struct{}
	appByHash map[types.Hash[*types.AppVote]]*voteSet[*types.Signed[*types.AppVote]]
	appQC     utils.Option[*types.AppQC]
}

func newRoad(commitQC *types.CommitQC, epoch *types.Epoch) *road {
	return &road{
		epoch:     epoch,
		commitQC:  commitQC,
		appByKey:  map[types.PublicKey]struct{}{},
		appByHash: map[types.Hash[*types.AppVote]]*voteSet[*types.Signed[*types.AppVote]]{},
	}
}

// Returns qc if a new qc has been reached.
func (r *road) pushAppVote(vote *types.Signed[*types.AppVote]) bool {
	if r.appQC.IsPresent() {
		return false
	}
	k := vote.Key()
	if _, ok := r.appByKey[k]; ok {
		return false
	}
	r.appByKey[k] = struct{}{}
	byHash, ok := r.appByHash[vote.Hash()]
	if !ok {
		if len(r.appByHash) == 1 {
			// Log an error just at the first conflicting hash.
			logger.Error("appHash mismatch", slog.Uint64("n", uint64(vote.Msg().Proposal().RoadIndex())))
		}
		byHash = &voteSet[*types.Signed[*types.AppVote]]{}
		r.appByHash[vote.Hash()] = byHash
	}
	c := r.epoch.Committee()
	byHash.weight += c.Weight(k)
	byHash.votes = append(byHash.votes, vote)
	if byHash.weight >= c.AppQuorum() {
		r.appQC = utils.Some(types.NewAppQC(byHash.votes))
		return true
	}
	return false
}
