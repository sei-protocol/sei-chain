package gov

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var _ precompiles.EndBlocker = Precompile{}

var (
	errEscrowShort    = errors.New("escrow holds less than the deposit")
	errRefundOverflow = errors.New("refund would overflow the depositor's balance")
)

// endBlockBudget bounds each of one EndBlock's two kinds of work, dropping
// proposals and ending votes, as x/gov's MaxVotesProcessedPerBlock does, in the
// units dropCost, settleCost and one per counted vote measure. Work past its
// budget waits for the next EndBlock; work larger than a whole budget runs when
// nothing has spent that budget, and uses it up.
const endBlockBudget = 1_000

// EndBlock drops proposals whose deposit period ended short of the minimum
// deposit, then tallies and settles those whose voting period is over, each in
// order of end time, then id, as x/gov's EndBlocker does. Its failures are
// proposals that failed: passed proposals that could not execute, deposits the
// escrow could not settle, and proposals whose stored data broke processing,
// which are marked Failed. It returns no error.
func (p Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	s := newStore(state)
	var failures []error
	dropDueProposals(s, block.UnixNano, &failures)
	budget := uint64(endBlockBudget)
	for id, end, ok := s.votingQueue().due(block.UnixNano); ok; id, end, ok = s.votingQueue().due(block.UnixNano) {
		settled := contained(s, s.votingQueue(), id, end, &failures, func() bool {
			return p.endVoting(block, state, s, id, end, &budget, &failures)
		})
		if !settled {
			break
		}
	}
	return failures, nil
}

// dropDueProposals drops proposals whose deposit period ended by now, within a
// budget of its own so that drops never delay ending votes.
func dropDueProposals(s store, now uint64, failures *[]error) {
	budget := uint64(endBlockBudget)
	for id, end, ok := s.depositQueue().due(now); ok; id, end, ok = s.depositQueue().due(now) {
		dropped := contained(s, s.depositQueue(), id, end, failures, func() bool {
			if !take(&budget, dropCost(s, id)) {
				return false
			}
			dropProposal(s, id, end, failures)
			return true
		})
		if !dropped {
			return
		}
	}
}

// contained runs step, one proposal's EndBlock work, reporting whether it
// finished. A panic in step fails proposal id instead: it is reported in
// failures, leaves queue q, where it is keyed by end, and is marked Failed.
// Work on q stops for the block when the proposal cannot leave it.
func contained(s store, q proposalQueue, id, end uint64, failures *[]error, step func() bool) (finished bool) {
	defer func() {
		if r := recover(); r != nil {
			*failures = append(*failures, fmt.Errorf("proposal %d failed: %v", id, r))
			finished = failProposal(s, q, id, end)
		}
	}()
	return step()
}

// failProposal removes proposal id from queue q and marks it Failed, reporting
// whether it left q.
func failProposal(s store, q proposalQueue, id, end uint64) (left bool) {
	defer func() {
		if recover() != nil {
			left = false // a record too broken to update stays as it is
		}
	}()
	q.remove(id, end)
	if prop, ok := s.proposal(id); ok {
		prop.status = StatusFailed
		s.setProposal(id, prop)
	}
	return !q.contains(id, end)
}

// dropCost is the budget dropping proposal id costs: it clears every depositor
// and every word of the proposal's content.
func dropCost(s store, id uint64) uint64 {
	return 1 + s.depositors(id).Len() + s.proposalContentChunks(id)
}

// settleCost is the budget finishing proposal id's tally costs: it credits every
// bonded validator, settles every depositor, and reads and executes the
// proposal's content, one unit per content word.
func settleCost(s store, id uint64, snap electorateSnapshot) uint64 {
	return uint64(len(snap.validators)) + 1 + s.depositors(id).Len() + s.proposalContentChunks(id)
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
func dropProposal(s store, id, end uint64, failures *[]error) {
	settleDeposits(s, id, true, failures)
	s.depositQueue().remove(id, end)
	s.deleteProposal(id)
}

// endVoting counts proposal id's votes and, once all are counted, settles it
// and removes it from the voting queue, where it is keyed by end, reporting
// whether it settled within budget. A failed expedited proposal becomes a
// regular one with a fresh round of voting for the rest of the regular period.
// A passed proposal that fails to execute adds its error to failures.
func (p Precompile) endVoting(block precompiles.BlockContext, state precompiles.State, s store, id, end uint64, budget *uint64, failures *[]error) bool {
	prop, ok := s.proposal(id)
	if !ok {
		s.votingQueue().remove(id, end)
		return true
	}
	snap := snapshotElectorate(state, p.Electorate)
	if !countVotes(state, p.Electorate, s, id, snap, budget) {
		return false
	}
	if !take(budget, settleCost(s, id, snap)) {
		return false
	}
	params := s.params()
	outcome := finishTally(state, p.Electorate, s, id, prop, params, snap)

	s.votingQueue().remove(id, end)
	if !prop.isExpedited || outcome.passes {
		settleDeposits(s, id, outcome.burnDeposits, failures)
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
	return true
}

// settleDeposits releases every deposit on proposal id from escrow, burning it
// or refunding it to its depositor, and clears the deposit records. A deposit
// the escrow cannot cover, or whose refund would overflow the depositor's
// balance, stays in escrow and is reported in failures.
func settleDeposits(s store, id uint64, burn bool, failures *[]error) {
	depositors := s.depositors(id)
	for n := depositors.Len(); n > 0; n-- {
		member := depositors.At(n - 1)
		depositor := solstorage.MemberAddress(member)
		amount := s.deposit(id, depositor)
		if err := releaseDeposit(s, depositor, amount, burn); err != nil {
			*failures = append(*failures, fmt.Errorf("proposal %d deposit from %s: %w", id, depositor, err))
		}
		s.setDeposit(id, depositor, new(big.Int))
		depositors.Remove(member)
	}
}

// releaseDeposit moves amount out of escrow, to depositor unless burn, failing
// without moving anything when the escrow or the depositor's balance cannot
// take it.
func releaseDeposit(s store, depositor common.Address, amount *big.Int, burn bool) error {
	if s.State.GetBalance(DepositEscrow).Cmp(amount) < 0 {
		return errEscrowShort
	}
	if !burn && new(big.Int).Add(s.State.GetBalance(depositor), amount).BitLen() > 256 {
		return errRefundOverflow
	}
	if err := s.State.SubBalance(DepositEscrow, amount); err != nil {
		return err
	}
	if !burn {
		s.State.AddBalance(depositor, amount)
	}
	return nil
}
