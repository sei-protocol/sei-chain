package consensus

import (
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
)

type timeoutVotes struct {
	byKey  map[types.PublicKey]*types.FullTimeoutVote
	byView map[types.View]map[types.PublicKey]*types.FullTimeoutVote
	qc     utils.AtomicSend[utils.Option[*types.TimeoutQC]]
}

func newTimeoutVotes() *timeoutVotes {
	return &timeoutVotes{
		byKey:  map[types.PublicKey]*types.FullTimeoutVote{},
		byView: map[types.View]map[types.PublicKey]*types.FullTimeoutVote{},
		qc:     utils.NewAtomicSend(utils.None[*types.TimeoutQC]()),
	}
}

func (tv *timeoutVotes) pushVote(c *types.Committee, vote *types.FullTimeoutVote) {
	// TODO: verify the vote.
	key := vote.Vote().Key()
	view := vote.Vote().Msg().View()
	if old, ok := tv.byKey[key]; ok {
		// Check if the old vote is newer than the new one.
		oldView := old.Vote().Msg().View()
		if !oldView.Less(view) {
			return
		}
		// Prune the old vote.
		delete(tv.byView[oldView], key)
		if len(tv.byView[oldView]) == 0 {
			delete(tv.byView, oldView)
		}
	}
	// Insert the new vote.
	tv.byKey[key] = vote
	if _, ok := tv.byView[view]; !ok {
		tv.byView[view] = map[types.PublicKey]*types.FullTimeoutVote{}
	}
	tv.byView[view][key] = vote
	// Check if we have enough votes for a TimeoutQC.
	if len(tv.byView[view]) < c.TimeoutQuorum() {
		return
	}
	// Construct a TimeoutQC from the votes.
	old := tv.qc.Load()
	if old, ok := old.Get(); ok && !old.View().Less(view) {
		return
	}
	var votes []*types.FullTimeoutVote
	for _, v := range tv.byView[view] {
		votes = append(votes, v)
	}
	tv.qc.Store(utils.Some(types.NewTimeoutQC(votes)))
}
