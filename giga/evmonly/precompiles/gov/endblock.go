package gov

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var _ precompiles.EndBlocker = Precompile{}

// endBlockBudget bounds one EndBlock's work, as x/gov's
// MaxVotesProcessedPerBlock does. Dropping a proposal costs one unit plus one
// per depositor; counting a vote costs one; finishing a tally costs one per
// bonded validator plus the drop cost. Work past the budget waits for the next
// EndBlock, and work larger than the budget runs alone in one.
const endBlockBudget = 1_000

// EndBlock drops proposals whose deposit period ended short of the minimum
// deposit, then tallies and settles those whose voting period is over, each in
// order of end time, then id, as x/gov's EndBlocker does.
//
// TODO(giga upgrade): apply the plan at its height and record appliedPlanHeight.
func (p Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) error {
	s := newStore(state)
	budget := uint64(endBlockBudget)
	for id, ok := s.depositQueue().due(block.Time); ok; id, ok = s.depositQueue().due(block.Time) {
		if !take(&budget, 1+s.depositors(id).Len()) {
			return nil
		}
		if err := dropProposal(s, id); err != nil {
			return err
		}
	}
	for id, ok := s.votingQueue().due(block.Time); ok; id, ok = s.votingQueue().due(block.Time) {
		settled, err := p.endVoting(block, state, s, id, &budget)
		if err != nil || !settled {
			return err
		}
	}
	return nil
}

// take spends n of budget, reporting whether the work may run. Work larger than
// the whole budget runs only on a budget nothing else has spent, and uses it up.
func take(budget *uint64, n uint64) bool {
	switch {
	case n <= *budget:
		*budget -= n
	case *budget == endBlockBudget:
		*budget = 0
	default:
		return false
	}
	return true
}

// dropProposal burns proposal id's deposits and deletes it.
func dropProposal(s store, id uint64) error {
	prop, _ := s.proposal(id)
	if err := settleDeposits(s, id, true); err != nil {
		return err
	}
	s.depositQueue().remove(id, prop.depositEndTime)
	s.deleteProposal(id)
	return nil
}

// endVoting counts proposal id's votes and, once all are counted, settles it,
// reporting whether it settled within budget. A failed expedited proposal
// becomes a regular one with a fresh round of voting for the rest of the
// regular period.
func (p Precompile) endVoting(block precompiles.BlockContext, state precompiles.State, s store, id uint64, budget *uint64) (bool, error) {
	snap := snapshotElectorate(state, p.Electorate)
	if !countVotes(state, p.Electorate, s, id, snap, budget) {
		return false, nil
	}
	if !take(budget, uint64(len(snap.validators))+1+s.depositors(id).Len()) {
		return false, nil
	}
	prop, _ := s.proposal(id)
	params := s.params()
	outcome := finishTally(state, p.Electorate, s, id, prop, params, snap)

	s.votingQueue().remove(id, prop.votingEndTime)
	if !prop.isExpedited || outcome.passes {
		if err := settleDeposits(s, id, outcome.burnDeposits); err != nil {
			return false, err
		}
	}
	switch {
	case outcome.passes:
		prop.status = StatusPassed
		if executeProposal(block, s, id) != nil {
			prop.status = StatusFailed
		}
	case prop.isExpedited:
		prop.isExpedited = false
		prop.votingEndTime = block.Time + params.VotingPeriod - min(params.ExpeditedVotingPeriod, params.VotingPeriod)
		s.votingQueue().add(id, prop.votingEndTime)
	default:
		prop.status = StatusRejected
	}
	prop.tallyYes = outcome.tokens(OptionYes)
	prop.tallyAbstain = outcome.tokens(OptionAbstain)
	prop.tallyNo = outcome.tokens(OptionNo)
	prop.tallyNoWithVeto = outcome.tokens(OptionNoWithVeto)
	s.setProposal(id, prop)
	return true, nil
}

// settleDeposits releases every deposit on proposal id from escrow, burning it
// or refunding it to its depositor, and clears the deposit records.
func settleDeposits(s store, id uint64, burn bool) error {
	depositors := s.depositors(id)
	for n := depositors.Len(); n > 0; n-- {
		member := depositors.At(n - 1)
		depositor := solstorage.MemberAddress(member)
		amount := s.deposit(id, depositor)
		if err := s.State.SubBalance(DepositEscrow, amount); err != nil {
			return err
		}
		if !burn {
			s.State.AddBalance(depositor, amount)
		}
		s.setDeposit(id, depositor, new(big.Int))
		depositors.Remove(member)
		s.proposalsDepositedBy(depositor).Remove(solstorage.Slot(id))
	}
	return nil
}
