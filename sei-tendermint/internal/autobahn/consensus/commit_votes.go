package consensus

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type scv = *types.Signed[*types.CommitVote]
type hcv = types.Hash[*types.CommitVote]

type commitVotes struct {
	byKey  map[types.PublicKey]scv
	byHash map[hcv]map[types.PublicKey]scv
	qc     utils.AtomicSend[utils.Option[*types.CommitQC]]
}

func newCommitVotes() *commitVotes {
	return &commitVotes{
		byKey:  map[types.PublicKey]scv{},
		byHash: map[hcv]map[types.PublicKey]scv{},
		qc:     utils.NewAtomicSend(utils.None[*types.CommitQC]()),
	}
}

func (cv *commitVotes) pushVote(c *types.Committee, vote *types.Signed[*types.CommitVote]) {
	key := vote.Key()
	view := vote.Msg().Proposal().View()

	// Check if the key has already voted.
	if oldVote, exists := cv.byKey[key]; exists {
		oldView := oldVote.Msg().Proposal().View()
		if !oldView.Less(view) {
			return // Ignore older or equal votes.
		}
		// Remove the old vote from the view map.
		h := oldVote.Hash()
		delete(cv.byHash[h], key)
		if len(cv.byHash[h]) == 0 {
			delete(cv.byHash, h)
		}
	}

	// Insert the new vote.
	cv.byKey[key] = vote
	h := vote.Hash()
	if _, exists := cv.byHash[h]; !exists {
		cv.byHash[h] = map[types.PublicKey]scv{}
	}
	cv.byHash[h][key] = vote

	// Check if we have enough votes for a CommitQC.
	if len(cv.byHash[h]) < c.CommitQuorum() {
		return
	}

	// Construct a CommitQC from the votes.
	old := cv.qc.Load()
	if old, ok := old.Get(); ok && !old.Proposal().View().Less(view) {
		return
	}
	var votes []*types.Signed[*types.CommitVote]
	for _, v := range cv.byHash[h] {
		votes = append(votes, v)
	}
	cv.qc.Store(utils.Some(types.NewCommitQC(votes)))
}
