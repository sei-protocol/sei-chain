package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneVoteSet caches Current-epoch weight for one hash.
// header survives reweight; qc is set at quorum.
type laneVoteSet struct {
	weight uint64
	votes  []*types.Signed[*types.LaneVote]
	qc     *types.LaneQC
	header *types.BlockHeader
}

func (s *laneVoteSet) reset() {
	s.weight = 0
	s.votes = s.votes[:0]
	s.qc = nil
}

func (s *laneVoteSet) add(weight, quorum uint64, vote *types.Signed[*types.LaneVote]) utils.Option[*types.LaneQC] {
	if s.qc != nil {
		return utils.None[*types.LaneQC]()
	}
	s.weight += weight
	s.votes = append(s.votes, vote)
	if s.weight < quorum {
		return utils.None[*types.LaneQC]()
	}
	s.qc = types.NewLaneQC(s.votes)
	return utils.Some(s.qc)
}

// blockVotes weights Current only; reweight on epoch advance.
type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*laneVoteSet
}

func newBlockVotes() blockVotes {
	return blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*laneVoteSet{},
	}
}

// pushVote stores vote under ep; zero-weight signers stay in byKey for reweight.
func (bv blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) utils.Option[*types.LaneQC] {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return utils.None[*types.LaneQC]()
	}
	bv.byKey[k] = vote

	h := vote.Msg().Header().Hash()
	set, ok := bv.byHash[h]
	if !ok {
		set = &laneVoteSet{header: vote.Msg().Header()}
		bv.byHash[h] = set
	}
	w := ep.Committee().Weight(k)
	if w == 0 {
		return utils.None[*types.LaneQC]()
	}
	return set.add(w, ep.Committee().LaneQuorum(), vote)
}

// reweight recomputes byHash from byKey; returns true if any new quorum.
func (bv blockVotes) reweight(newEpoch *types.Epoch) bool {
	c := newEpoch.Committee()
	for _, set := range bv.byHash {
		set.reset()
	}
	quorumReached := false
	for k, vote := range bv.byKey {
		w := c.Weight(k)
		if w == 0 {
			continue
		}
		set := bv.byHash[vote.Msg().Header().Hash()]
		if set.add(w, c.LaneQuorum(), vote).IsPresent() {
			quorumReached = true
		}
	}
	return quorumReached
}

func (bv blockVotes) laneQC() utils.Option[*types.LaneQC] {
	for _, set := range bv.byHash {
		if set.qc != nil {
			return utils.Some(set.qc)
		}
	}
	return utils.None[*types.LaneQC]()
}
