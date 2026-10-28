package gov

import (
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var _ precompiles.EndBlocker = Precompile{}

// endBlockBudget bounds each of one EndBlock's two kinds of work, dropping
// proposals and ending votes, as x/gov's MaxVotesProcessedPerBlock does.
// Dropping a proposal costs one unit plus one per depositor; counting a vote
// costs one; finishing a tally costs one per bonded validator, one per param
// change the proposal makes, plus the drop cost. Work past its budget waits for
// the next EndBlock, and work larger than the budget runs alone in one.
const endBlockBudget = 1_000

// EndBlock drops proposals whose deposit period ended short of the minimum
// deposit, then tallies and settles those whose voting period is over, each in
// order of end time, then id, as x/gov's EndBlocker does. Its failures are
// passed proposals that failed to execute.
//
// TODO(giga upgrade): apply the plan at its height and record appliedPlanHeight.
func (p Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	s := newStore(state)
	if err := dropDueProposals(s, block.UnixNano); err != nil {
		return nil, err
	}
	budget := uint64(endBlockBudget)
	var failures []error
	for id, end, ok := s.votingQueue().due(block.UnixNano); ok; id, end, ok = s.votingQueue().due(block.UnixNano) {
		settled, err := p.endVoting(block, state, s, id, end, &budget, &failures)
		if err != nil || !settled {
			return failures, err
		}
	}
	return failures, nil
}

// dropDueProposals drops proposals whose deposit period ended by now, within a
// budget of its own so that drops never delay ending votes.
func dropDueProposals(s store, now uint64) error {
	budget := uint64(endBlockBudget)
	for id, end, ok := s.depositQueue().due(now); ok; id, end, ok = s.depositQueue().due(now) {
		if !take(&budget, 1+s.depositors(id).Len()) {
			return nil
		}
		if err := dropProposal(s, id, end); err != nil {
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

// dropProposal burns proposal id's deposits, deletes it, and removes it from
// the deposit queue, where it is keyed by end.
func dropProposal(s store, id, end uint64) error {
	if err := settleDeposits(s, id, true); err != nil {
		return err
	}
	s.depositQueue().remove(id, end)
	s.deleteProposal(id)
	return nil
}

// endVoting counts proposal id's votes and, once all are counted, settles it
// and removes it from the voting queue, where it is keyed by end, reporting
// whether it settled within budget. A failed expedited proposal becomes a
// regular one with a fresh round of voting for the rest of the regular period.
// A passed proposal that fails to execute adds its error to failures.
func (p Precompile) endVoting(block precompiles.BlockContext, state precompiles.State, s store, id, end uint64, budget *uint64, failures *[]error) (bool, error) {
	prop, ok := s.proposal(id)
	if !ok {
		s.votingQueue().remove(id, end)
		return true, nil
	}
	snap := snapshotElectorate(state, p.Electorate)
	if !countVotes(state, p.Electorate, s, id, snap, budget) {
		return false, nil
	}
	if !take(budget, uint64(len(snap.validators))+executionCost(s, id)+1+s.depositors(id).Len()) {
		return false, nil
	}
	params := s.params()
	outcome := finishTally(state, p.Electorate, s, id, prop, params, snap)

	s.votingQueue().remove(id, end)
	if !prop.isExpedited || outcome.passes {
		if err := settleDeposits(s, id, outcome.burnDeposits); err != nil {
			return false, err
		}
	}
	switch {
	case outcome.passes:
		prop.status = StatusPassed
		if err := executeProposal(block, s, id); err != nil {
			prop.status = StatusFailed
			*failures = append(*failures, fmt.Errorf("proposal %d failed to execute: %w", id, err))
		}
	case prop.isExpedited:
		prop.isExpedited = false
		prop.votingEndTime = block.UnixNano + params.VotingPeriod - min(params.ExpeditedVotingPeriod, params.VotingPeriod)
		s.votingQueue().add(id, prop.votingEndTime)
	default:
		prop.status = StatusRejected
	}
	prop.recordTally(outcome)
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
