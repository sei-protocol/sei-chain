package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneVoteSet holds weight toward LaneQC for one header hash under Current.
type laneVoteSet struct {
	weight uint64
	votes  []*types.Signed[*types.LaneVote]
	header *types.BlockHeader
	qc     utils.Option[*types.LaneQC]
}

// add credits vote weight until quorum. Returns a newly formed LaneQC iff this
// vote crosses quorum and caches it for subsequent readers.
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

func (s *laneVoteSet) laneQC() utils.Option[*types.LaneQC] {
	return s.qc
}

// blockVotes credits lane votes under the Current committee only.
// Each header hash may form its own LaneQC.
type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*laneVoteSet
}

func newBlockVotes() *blockVotes {
	return &blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*laneVoteSet{},
	}
}

// pushVote credits vote under ep (Current). Zero-weight → drop (not retained).
// Callers VerifySig first; after a lock release, Weight==0 is still a silent drop.
func (bv *blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) utils.Option[*types.LaneQC] {
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
// Zero-weight signers are removed from byKey. Callers wake waiters via
// ctrl.Updated() after advanceEpoch (not via a return flag).
func (bv *blockVotes) reweight(newEpoch *types.Epoch) {
	c := newEpoch.Committee()
	clear(bv.byHash)
	quorum := c.LaneQuorum()
	for k, vote := range bv.byKey {
		w := c.Weight(k)
		if w == 0 {
			delete(bv.byKey, k)
			continue
		}
		h := vote.Msg().Header().Hash()
		set, ok := bv.byHash[h]
		if !ok {
			set = &laneVoteSet{header: vote.Msg().Header()}
			bv.byHash[h] = set
		}
		set.add(w, quorum, vote)
	}
}

func (bv *blockVotes) laneQC() utils.Option[*types.LaneQC] {
	for _, set := range bv.byHash {
		if qc, ok := set.laneQC().Get(); ok {
			return utils.Some(qc)
		}
	}
	return utils.None[*types.LaneQC]()
}
