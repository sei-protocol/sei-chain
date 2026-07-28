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
}

func (s *laneVoteSet) reset() {
	s.weight = 0
	s.votes = s.votes[:0]
}

// add credits vote weight. Returns true if weight reached quorum (may already
// have been at/above quorum from a prior add).
func (s *laneVoteSet) add(weight, quorum uint64, vote *types.Signed[*types.LaneVote]) bool {
	s.weight += weight
	s.votes = append(s.votes, vote)
	return s.weight >= quorum
}

// blockVotes credits lane votes under the Current committee only.
// At most one LaneQC is retained for the block (not one per header hash).
type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*laneVoteSet
	qc     utils.Option[*types.LaneQC]
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

	// One QC per block: still retain the vote for reweight, but stop growing sets.
	if bv.qc.IsPresent() {
		return utils.None[*types.LaneQC]()
	}

	h := vote.Msg().Header().Hash()
	set, ok := bv.byHash[h]
	if !ok {
		set = &laneVoteSet{header: vote.Msg().Header()}
		bv.byHash[h] = set
	}
	if !set.add(w, ep.Committee().LaneQuorum(), vote) {
		return utils.None[*types.LaneQC]()
	}
	bv.qc = utils.Some(types.NewLaneQC(set.votes))
	return bv.qc
}

// reweight recomputes already-stored votes under new Current after advanceEpoch.
// Zero-weight signers are removed from byKey. Callers wake waiters via
// ctrl.Updated() after advanceEpoch (not via a return flag).
func (bv *blockVotes) reweight(newEpoch *types.Epoch) {
	c := newEpoch.Committee()
	bv.qc = utils.None[*types.LaneQC]()
	for _, set := range bv.byHash {
		set.reset()
	}
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
		if set.add(w, c.LaneQuorum(), vote) && !bv.qc.IsPresent() {
			bv.qc = utils.Some(types.NewLaneQC(set.votes))
		}
	}
}

func (bv *blockVotes) laneQC() utils.Option[*types.LaneQC] {
	return bv.qc
}
