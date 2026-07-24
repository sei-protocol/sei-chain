package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneVoteSet holds weight toward LaneQC for one header hash under Current.
type laneVoteSet struct {
	weight uint64
	votes  []*types.Signed[*types.LaneVote]
	qc     utils.Option[*types.LaneQC]
	header *types.BlockHeader
}

func (s *laneVoteSet) reset() {
	s.weight = 0
	s.votes = s.votes[:0]
	s.qc = utils.None[*types.LaneQC]()
}

func (s *laneVoteSet) add(weight, quorum uint64, vote *types.Signed[*types.LaneVote]) utils.Option[*types.LaneQC] {
	if s.qc.IsPresent() {
		return utils.None[*types.LaneQC]()
	}
	s.weight += weight
	s.votes = append(s.votes, vote)
	if s.weight < quorum {
		return utils.None[*types.LaneQC]()
	}
	s.qc = utils.Some(types.NewLaneQC(s.votes))
	return s.qc
}

// blockVotes credits lane votes under the Current committee only.
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

// pushVote credits vote under ep (Current). Zero-weight → drop (not retained).
// Callers VerifySig first; after a lock release, Weight==0 is still a silent drop.
func (bv blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) utils.Option[*types.LaneQC] {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return utils.None[*types.LaneQC]()
	}
	w := ep.Committee().Weight(k)
	if w == 0 {
		return utils.None[*types.LaneQC]()
	}
	bv.byKey[k] = vote

	h := vote.Msg().Header().Hash()
	set, ok := bv.byHash[h]
	if !ok {
		set = &laneVoteSet{header: vote.Msg().Header()}
		bv.byHash[h] = set
	}
	return set.add(w, ep.Committee().LaneQuorum(), vote)
}

// reweight recomputes already-stored votes under new Current after advanceEpoch.
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
		if set.qc.IsPresent() {
			return set.qc
		}
	}
	return utils.None[*types.LaneQC]()
}
