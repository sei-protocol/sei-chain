package consensus

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type spv = *types.Signed[*types.PrepareVote]
type hpv = types.Hash[*types.PrepareVote]

// prepareVotes holds the votes for the prepare phase of consensus.
type prepareVotes struct {
	byKey  map[types.PublicKey]spv
	byHash map[hpv]map[types.PublicKey]spv
	qc     utils.AtomicSend[utils.Option[*types.PrepareQC]]
}

// newPrepareVotes initializes a new prepareVotes instance.
func newPrepareVotes() *prepareVotes {
	return &prepareVotes{
		byKey:  map[types.PublicKey]spv{},
		byHash: map[hpv]map[types.PublicKey]spv{},
		qc:     utils.NewAtomicSend(utils.None[*types.PrepareQC]()),
	}
}

// pushVote processes a new prepare vote and updates the prepare votes state.
func (pv *prepareVotes) pushVote(c *types.Committee, vote *types.Signed[*types.PrepareVote]) {
	key := vote.Key()
	view := vote.Msg().Proposal().View()

	// Check if the key has already voted.
	if oldVote, exists := pv.byKey[key]; exists {
		oldView := oldVote.Msg().Proposal().View()
		if !oldView.Less(view) {
			return // Ignore older or equal votes.
		}
		// Remove the old vote from the view map.
		h := oldVote.Hash()
		delete(pv.byHash[h], key)
		if len(pv.byHash[h]) == 0 {
			delete(pv.byHash, h)
		}
	}

	// Insert the new vote.
	pv.byKey[key] = vote
	h := vote.Hash()
	if _, exists := pv.byHash[h]; !exists {
		pv.byHash[h] = map[types.PublicKey]spv{}
	}
	pv.byHash[h][key] = vote

	// Check if we have enough votes for a PrepareQC.
	if len(pv.byHash[h]) < c.PrepareQuorum() {
		return
	}

	// Construct a PrepareQC from the votes.
	if old, ok := pv.qc.Load().Get(); ok && !old.Proposal().View().Less(view) {
		return
	}
	var votes []*types.Signed[*types.PrepareVote]
	for _, v := range pv.byHash[h] {
		votes = append(votes, v)
	}
	pv.qc.Store(utils.Some(types.NewPrepareQC(votes)))
}
