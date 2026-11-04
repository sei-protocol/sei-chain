package staking

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

var (
	errNoBondedPower       = errors.New("the bonded set would have no power")
	errJailedInPowerIndex  = errors.New("jailed validator in the power index")
	errUnknownValidator    = errors.New("validator in the bonded set or power index does not exist")
	errPowerOverflow       = errors.New("consensus power does not fit 64 bits")
	errRedelegationCounted = errors.New("redelegation entry was not counted")
)

// endBlockBudget bounds the entries one EndBlock completes from each queue: one
// unit per entry, plus one per unbonding entry of the delegation it pays.
// Entries past the budget wait for the next EndBlock; an entry larger than the
// whole budget runs when nothing has spent it, and uses it up. Queuing an
// unbonding or redelegation entry writes at least five new slots, over 100,000
// gas, so one EndBlock completes more entries than 300M gas of calls can queue.
const endBlockBudget = 3_000

// EndBlock updates the bonded set, then completes the validators, unbonding
// entries and redelegation entries whose unbonding period is over, in that
// order, as x/staking's BlockValidatorUpdates does. Before staking's params are
// written it does nothing. Its failures are a bonded set that could not be
// updated, which is kept as it was, and queue entries whose stored data does
// not add up, which are dropped. It returns no error.
func (p Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if !chainparams.HasStaking(state) {
		return nil, nil
	}
	s := newStore(state)
	params := s.params()
	var failures []error
	if err := p.updateBondedSet(block, s, params); err != nil {
		failures = append(failures, fmt.Errorf("%w: bonded set kept as it was: %w", precompiles.ErrDegraded, err))
	}
	drain(block, s.validatorQueue(), "validator", &failures,
		func(common.Hash) uint64 { return 1 },
		func(e common.Hash) error { return p.completeValidatorUnbonding(block, s, e) })
	drain(block, s.unbondingQueue(), "unbonding", &failures,
		func(e common.Hash) uint64 {
			delegator, op := unbondingQueueEntry(s, e)
			return 1 + s.unbondingEntryCount(delegator, op)
		},
		func(e common.Hash) error { return completeUnbonding(block, s, e) })
	drain(block, s.redelegationQueue(), "redelegation", &failures,
		func(common.Hash) uint64 { return 1 },
		func(e common.Hash) error { return completeRedelegation(s, e) })
	return failures, nil
}

// updateBondedSet applies the bonded set the power index ranks, as x/staking's
// ApplyAndReturnValidatorSetUpdates does. When that fails, or would leave the
// set with no power, it writes nothing.
func (p Precompile) updateBondedSet(block precompiles.BlockContext, s store, params chainparams.Staking) error {
	snap := s.State.Snapshot()
	if err := p.applyBondedSet(block, s, params); err != nil {
		s.State.RevertToSnapshot(snap)
		return err
	}
	return nil
}

// applyBondedSet bonds the first MaxValidators validators of the power index
// that have power, begins unbonding those of the last set that are no longer
// among them, in address order, moves their tokens between the escrows, and
// stores the new set and its total power when it differs from the last.
func (p Precompile) applyBondedSet(block precompiles.BlockContext, s store, params chainparams.Staking) error {
	last, err := s.lastValidators()
	if err != nil {
		return err
	}
	lastPower := make(map[common.Address]uint64, len(last))
	for _, v := range last {
		lastPower[v.operator] = v.power
	}

	changed := false
	next := make([]validatorPower, 0, params.MaxValidators)
	total, toBonded, toNotBonded := new(big.Int), new(big.Int), new(big.Int)
	index := s.powerIndex()
	for i, n := uint64(0), index.len(); i < n && len(next) < int(params.MaxValidators); i++ {
		op := index.at(i)
		v, ok := s.validator(op)
		switch {
		case !ok:
			return fmt.Errorf("%w: %s", errUnknownValidator, op)
		case v.jailed:
			return fmt.Errorf("%w: %s", errJailedInPowerIndex, op)
		}
		power := consensusPower(v.tokens)
		if power.Sign() == 0 {
			break
		}
		if !power.IsUint64() {
			return fmt.Errorf("%w: %s", errPowerOverflow, op)
		}
		if !v.bonded() {
			if err := p.bond(block, s, op, &v); err != nil {
				return err
			}
			toBonded.Add(toBonded, v.tokens)
		}
		if old, ok := lastPower[op]; !ok || old != power.Uint64() {
			changed = true
		}
		delete(lastPower, op)
		next = append(next, validatorPower{operator: op, power: power.Uint64()})
		total.Add(total, power)
	}

	gone := slices.SortedFunc(maps.Keys(lastPower), func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	for _, op := range gone {
		v, ok := s.validator(op)
		if !ok {
			return fmt.Errorf("%w: %s", errUnknownValidator, op)
		}
		if err := p.beginUnbonding(block, s, op, &v, params.UnbondingTime); err != nil {
			return err
		}
		toNotBonded.Add(toNotBonded, v.tokens)
		changed = true
	}
	if total.Sign() == 0 && len(last) > 0 {
		return errNoBondedPower
	}

	switch toBonded.Cmp(toNotBonded) {
	case 1:
		err = transfer(s.State, NotBondedEscrow, BondedEscrow, toBonded.Sub(toBonded, toNotBonded))
	case -1:
		err = transfer(s.State, BondedEscrow, NotBondedEscrow, toNotBonded.Sub(toNotBonded, toBonded))
	}
	if err != nil {
		return err
	}
	if changed {
		s.setLastValidators(next)
		s.setLastTotalPower(total)
	}
	return nil
}

// drain completes each entry due in q, in queue order, under a budget of its
// own, until none is due or the next does not fit what is left. Each entry is
// removed from q once completed. A completion that fails is reverted and its
// entry removed all the same, so it holds up no other, and the failure is
// reported. A queue whose head is past its tail is reported and left alone.
func drain(block precompiles.BlockContext, q queue, name string, failures *[]error, cost func(e common.Hash) uint64, complete func(e common.Hash) error) {
	if err := q.check(); err != nil {
		*failures = append(*failures, fmt.Errorf("%w: %s queue: %w", precompiles.ErrDegraded, name, err))
		return
	}
	budget := uint64(endBlockBudget)
	for e, ok := q.due(block.UnixNano); ok; e, ok = q.due(block.UnixNano) {
		if !take(&budget, cost(e)) {
			return
		}
		snap := q.s.State.Snapshot()
		if err := complete(e); err != nil {
			q.s.State.RevertToSnapshot(snap)
			*failures = append(*failures, fmt.Errorf("%w: %s queue entry %d dropped: %w", precompiles.ErrDegraded, name, q.head(), err))
		}
		q.pop()
	}
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

// completeValidatorUnbonding makes the validator of validator queue entry e
// unbonded, as x/staking's UnbondAllMatureValidators does, and removes it when
// it has no shares left. An entry left by a validator that bonded again since
// is skipped.
func (p Precompile) completeValidatorUnbonding(block precompiles.BlockContext, s store, e common.Hash) error {
	op := s.AddressAt(at(e, validatorQueueOperator), validatorQueueOperator.offset)
	v, ok := s.validator(op)
	if !ok || v.status != statusUnbonding ||
		v.unbondingTime != s.Uint64(at(e, queueCompletionTime), queueCompletionTime.offset) ||
		v.unbondingHeight != s.Uint64(at(e, validatorQueueUnbondingHeight), validatorQueueUnbondingHeight.offset) {
		return nil
	}
	v.status = statusUnbonded
	s.setValidator(op, v)
	if v.delegatorShares.Sign() == 0 {
		return p.removeValidator(block, s, op, v)
	}
	return nil
}

// completeUnbonding pays the delegator of unbonding queue entry e every
// matured entry of its unbonding from the entry's validator, out of the
// not-bonded escrow, and removes them, as x/staking's CompleteUnbonding does.
func completeUnbonding(block precompiles.BlockContext, s store, e common.Hash) error {
	delegator, op := unbondingQueueEntry(s, e)
	entries, err := s.unbondingEntries(delegator, op)
	if err != nil {
		return err
	}
	kept := make([]unbondingEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.completionTime > block.UnixNano {
			kept = append(kept, entry)
			continue
		}
		if err := transfer(s.State, NotBondedEscrow, delegator, entry.balance); err != nil {
			return fmt.Errorf("paying %s from %s: %w", delegator, op, err)
		}
	}
	if len(kept) != len(entries) {
		s.setUnbondingEntries(delegator, op, kept)
	}
	return nil
}

// completeRedelegation uncounts redelegation queue entry e from its delegator's
// open redelegations, as x/staking's CompleteRedelegation removes the entry. It
// moves no tokens: they moved when the redelegation began.
func completeRedelegation(s store, e common.Hash) error {
	delegator := s.AddressAt(at(e, redelegationQueueDelegator), redelegationQueueDelegator.offset)
	src := s.AddressAt(at(e, redelegationQueueSrcValidator), redelegationQueueSrcValidator.offset)
	dst := s.AddressAt(at(e, redelegationQueueDstValidator), redelegationQueueDstValidator.offset)
	entries, received := s.redelegationEntryCount(delegator, src, dst), s.receivingRedelegations(dst, delegator)
	if entries.Sign() == 0 || received.Sign() == 0 {
		return fmt.Errorf("%w: %s from %s to %s", errRedelegationCounted, delegator, src, dst)
	}
	s.setRedelegationEntryCount(delegator, src, dst, entries.Sub(entries, big.NewInt(1)))
	s.setReceivingRedelegations(dst, delegator, received.Sub(received, big.NewInt(1)))
	return nil
}
