package avail

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LaneVotes have two jobs: byKey keeps headers for FullCommitQC; byHash
// accumulates applied-epoch weight for LaneQC. byKey is retained even after the
// latest CommitQC moves into a new epoch, since a FullCommitQC in the previous
// epoch may still need those headers — so we can't clear it on epoch change.
// qc is set once weight reaches quorum under the applied committee and cleared
// on reweight.
type blockVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.LaneVote]
	byHash map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]
	qc     utils.Option[*types.LaneQC]
	// qcCounted is set once this block has formed a LaneQC.
	qcCounted bool
	// firstVoteAt and qcAt are when the first vote arrived and the LaneQC formed, if they have.
	firstVoteAt, qcAt utils.Option[time.Time]
	// voteTimed and qcTimed are set once those two latencies are observed, so each is observed once.
	voteTimed, qcTimed bool
}

// untimed returns the first-vote and LaneQC times whose latency has not been observed yet, and marks
// them observed. The caller observes them against the block's creation time.
func (bv *blockVotes) untimed() (vote, qc utils.Option[time.Time]) {
	if at, ok := bv.firstVoteAt.Get(); ok && !bv.voteTimed {
		bv.voteTimed, vote = true, utils.Some(at)
	}
	if at, ok := bv.qcAt.Get(); ok && !bv.qcTimed {
		bv.qcTimed, qc = true, utils.Some(at)
	}
	return vote, qc
}

func newBlockVotes() *blockVotes {
	return &blockVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.LaneVote]{},
		byHash: map[types.BlockHeaderHash]*voteSet[*types.Signed[*types.LaneVote]]{},
	}
}

// return true iff the vote is newly stored in byKey.
func (bv *blockVotes) pushVote(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) bool {
	k := vote.Key()
	if _, ok := bv.byKey[k]; ok {
		return false
	}
	bv.byKey[k] = vote
	meters.LaneVotesIngested.Add(1)
	bv.credit(ep, vote)
	return true
}

func (bv *blockVotes) reweight(ep *types.Epoch) {
	bv.qc = utils.None[*types.LaneQC]()
	clear(bv.byHash)
	for _, vote := range bv.byKey {
		bv.credit(ep, vote)
	}
}

// credit adds the vote's weight to its header bucket and, on reaching quorum,
// forms the LaneQC. A block is counted as a LaneQC only the first time it forms one.
func (bv *blockVotes) credit(ep *types.Epoch, vote *types.Signed[*types.LaneVote]) {
	if bv.qc.IsPresent() {
		return
	}
	c := ep.Committee()
	k := vote.Key()
	w := c.Weight(k)
	if w == 0 {
		return
	}
	h := vote.Msg().Header().Hash()
	byHash, ok := bv.byHash[h]
	if !ok {
		byHash = &voteSet[*types.Signed[*types.LaneVote]]{}
		bv.byHash[h] = byHash
	}
	byHash.weight += w
	byHash.votes = append(byHash.votes, vote)
	if byHash.weight >= c.LaneQuorum() {
		bv.qc = utils.Some(types.NewLaneQC(byHash.votes))
		if !bv.qcCounted {
			bv.qcCounted = true
			meters.LaneQCs.Add(1)
		}
	}
}

func (bv *blockVotes) header(want types.BlockHeaderHash) utils.Option[*types.BlockHeader] {
	for _, vote := range bv.byKey {
		h := vote.Msg().Header()
		if h.Hash() == want {
			return utils.Some(h)
		}
	}
	return utils.None[*types.BlockHeader]()
}
