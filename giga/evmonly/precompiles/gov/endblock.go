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
	errEscrowShort           = errors.New("escrow holds less than the deposit")
	errRefundOverflow        = errors.New("refund would overflow the depositor's balance")
	errDepositOverTotal      = errors.New("deposits exceed the proposal's total deposit")
	errInvalidStoredVote     = errors.New("invalid stored vote")
	errInvalidStoredDeposits = errors.New("invalid stored deposits")
	errStuckInQueue          = errors.New("proposal did not leave its queue")
	errQueueCorrupt          = errors.New("queue holds more entries than proposals exist")
)

// endBlockBudget bounds each of one EndBlock's two kinds of work, dropping
// proposals and ending votes, as x/gov's MaxVotesProcessedPerBlock does. The
// units are those dropCost and settleCost return, one per counted vote, and
// failCost per failed proposal. Work past its budget waits for the next
// EndBlock; work larger than a whole budget runs when nothing has spent that
// budget, and uses it up.
const endBlockBudget = 1_000

// failCost is the budget failing a proposal in place costs.
const failCost = 1

// EndBlock drops proposals whose deposit period ended short of the minimum
// deposit, then tallies and settles those whose voting period is over, each in
// order of end time, then id, as x/gov's EndBlocker does. Its failures are
// passed proposals that could not execute, which are marked Failed, and
// proposals whose stored data does not add up, which are failed in place. It
// returns no error.
func (p Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	s := newStore(state)
	var failures []error
	drain(s, s.depositQueue(), block.UnixNano, &failures, func(id, end uint64, budget *uint64) (bool, error) {
		return dropProposal(s, id, end, budget)
	})
	drain(s, s.votingQueue(), block.UnixNano, &failures, func(id, end uint64, budget *uint64) (bool, error) {
		return p.endVoting(block, s, id, end, budget, &failures)
	})
	return failures, nil
}

// drain runs step on each proposal due in q, in queue order, under a budget of
// its own, until the queue holds nothing due or a step reports it did not
// finish. A step that fails, or that leaves its proposal at the head of q, is
// reverted and the proposal is failed in place, so it holds up no other
// proposal. A queue longer than the proposal ids ever assigned is reported and
// still drained.
func drain(s store, q proposalQueue, now uint64, failures *[]error, step func(id, end uint64, budget *uint64) (bool, error)) {
	if q.Len() > s.proposalIDCount() {
		*failures = append(*failures, fmt.Errorf("%w: %w: %d entries", precompiles.ErrDegraded, errQueueCorrupt, q.Len()))
	}
	budget := uint64(endBlockBudget)
	for id, end, ok := q.due(now); ok; id, end, ok = q.due(now) {
		snap := s.State.Snapshot()
		finished, err := step(id, end, &budget)
		if err == nil && !finished {
			return
		}
		if err == nil {
			if next, nextEnd, ok := q.due(now); ok && next == id && nextEnd == end {
				err = errStuckInQueue
			}
		}
		if err == nil {
			continue
		}
		s.State.RevertToSnapshot(snap)
		*failures = append(*failures, fmt.Errorf("%w: proposal %d: %w", precompiles.ErrDegraded, id, err))
		failInPlace(s, q, id, failures)
		if !take(&budget, failCost) {
			return
		}
	}
}

// failInPlace takes proposal id, the head of q, out of q and marks it Failed,
// refunding its deposits. When the refund fails it leaves the deposits in
// escrow, recorded, and reports why.
func failInPlace(s store, q proposalQueue, id uint64, failures *[]error) {
	snap := s.State.Snapshot()
	q.popHead()
	markFailed(s, id)
	if err := settleDeposits(s, id, false); err != nil {
		s.State.RevertToSnapshot(snap)
		*failures = append(*failures, fmt.Errorf("%w: proposal %d deposits left in escrow: %w", precompiles.ErrDegraded, id, err))
		q.popHead()
		markFailed(s, id)
	}
}

func markFailed(s store, id uint64) {
	if prop, ok := s.proposal(id); ok {
		prop.status = StatusFailed
		s.setProposal(id, prop)
	}
}

// dropCost is the budget dropping proposal id costs: one unit, plus one per
// depositor cleared and one per word of content cleared.
func dropCost(s store, id uint64) (uint64, error) {
	depositors, err := s.depositorCount(id)
	if err != nil {
		return 0, err
	}
	chunks, err := s.proposalContentChunks(id)
	if err != nil {
		return 0, err
	}
	return 1 + depositors + chunks, nil
}

// settleCost is the budget finishing proposal id's tally costs: one unit, plus
// one per bonded validator credited, one per depositor settled, and one per word
// of content read and executed.
func settleCost(s store, id uint64, snap electorateSnapshot) (uint64, error) {
	depositors, err := s.depositorCount(id)
	if err != nil {
		return 0, err
	}
	chunks, err := s.proposalContentChunks(id)
	if err != nil {
		return 0, err
	}
	return uint64(len(snap.validators)) + 1 + depositors + chunks, nil
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
// the deposit queue, where it is keyed by end, reporting whether the budget
// covered it.
func dropProposal(s store, id, end uint64, budget *uint64) (bool, error) {
	cost, err := dropCost(s, id)
	if err != nil {
		return false, err
	}
	if !take(budget, cost) {
		return false, nil
	}
	if err := settleDeposits(s, id, true); err != nil {
		return false, err
	}
	s.depositQueue().remove(id, end)
	return true, s.deleteProposal(id)
}

// endVoting counts proposal id's votes and, once all are counted, settles it
// and removes it from the voting queue, where it is keyed by end, reporting
// whether it settled within budget. A failed expedited proposal becomes a
// regular one with a fresh round of voting for the rest of the regular period.
// A passed proposal that fails to execute is marked Failed and adds its error to
// failures.
func (p Precompile) endVoting(block precompiles.BlockContext, s store, id, end uint64, budget *uint64, failures *[]error) (bool, error) {
	prop, ok := s.proposal(id)
	if !ok {
		s.votingQueue().remove(id, end)
		return true, nil
	}
	snap := snapshotElectorate(s.State, p.Electorate)
	if counted, err := countVotes(p.Electorate, s, id, snap, budget); err != nil || !counted {
		return false, err
	}
	cost, err := settleCost(s, id, snap)
	if err != nil {
		return false, err
	}
	if !take(budget, cost) {
		return false, nil
	}
	params := s.params()
	outcome, err := finishTally(p.Electorate, s, id, prop, params, snap)
	if err != nil {
		return false, err
	}

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
// or refunding it to its depositor, and clears the deposit records. It fails
// when the deposits do not add up: more depositors than allowed, deposits past
// the proposal's total, or a release the escrow or a depositor's balance cannot
// take.
func settleDeposits(s store, id uint64, burn bool) error {
	n, err := s.depositorCount(id)
	if err != nil {
		return err
	}
	remaining := new(big.Int)
	if prop, ok := s.proposal(id); ok {
		remaining.Set(prop.totalDeposit)
	}
	depositors := s.depositors(id)
	for ; n > 0; n-- {
		member := depositors.At(n - 1)
		depositor := solstorage.MemberAddress(member)
		amount := s.deposit(id, depositor)
		if amount.Cmp(remaining) > 0 {
			return fmt.Errorf("%w: %s from %s", errDepositOverTotal, amount, depositor)
		}
		remaining.Sub(remaining, amount)
		if err := releaseDeposit(s.State, depositor, amount, burn); err != nil {
			return fmt.Errorf("deposit from %s: %w", depositor, err)
		}
		s.setDeposit(id, depositor, new(big.Int))
		depositors.Remove(member)
	}
	return nil
}

// releaseDeposit moves amount out of escrow, to depositor unless burn, failing
// without moving anything when the escrow holds less than amount or a refund
// would overflow the depositor's balance.
func releaseDeposit(state precompiles.State, depositor common.Address, amount *big.Int, burn bool) error {
	if state.GetBalance(DepositEscrow).Cmp(amount) < 0 {
		return errEscrowShort
	}
	if !burn && new(big.Int).Add(state.GetBalance(depositor), amount).BitLen() > 256 {
		return errRefundOverflow
	}
	if err := state.SubBalance(DepositEscrow, amount); err != nil {
		return err
	}
	if !burn {
		state.AddBalance(depositor, amount)
	}
	return nil
}
