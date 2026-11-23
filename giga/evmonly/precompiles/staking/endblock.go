package staking

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math"
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

// endBlockBudget bounds the work one EndBlock does on each queue: one unit per
// entry, plus one per unbonding entry of the delegation an unbonding queue entry
// pays. Entries past the budget wait for the next EndBlock; an entry larger than
// the whole budget runs when nothing has spent it, and uses it up. A block that
// closes an epoch spends a second budget on the staged queue, applying entries
// to project its snapshot and then reverting them. Staging an operation writes
// at least three new slots, over 60,000 gas, so 300M gas of calls stage at most
// 5,000 a block, and applying one queues at most one unbonding or redelegation
// entry. The redelegation queue completes as many a block as the staged queue
// applies; the unbonding queue, whose entries cost at least two units,
// completes at most 1,500. A backlog in any of them waits for blocks with fewer
// calls.
const endBlockBudget = 3_000

// EndBlock advances the current epoch when the block closes one, applies the
// staged operations whose epoch has ended, updates the bonded set, snapshots the
// bonded set as it will be once the operations still staged take effect when the
// block closes an epoch, then completes the validators, unbonding entries and
// redelegation entries whose unbonding period is over, in that order, as
// x/staking's BlockValidatorUpdates does. Before staking's params are written it
// does nothing. Its failures are staged operations that could not be applied,
// which are dropped, a bonded set that could not be updated, which is kept as it
// was, a snapshot that could not be taken, and queue entries whose stored data
// does not add up, which are dropped. It returns no error.
func (p Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if !chainparams.HasStaking(state) {
		return nil, nil
	}
	s := newStore(state)
	params := s.params()
	var failures []error
	if epoch, ok := block.ClosingEpoch.Get(); ok {
		s.setCurrentEpoch(epoch + 1)
	}
	if current := s.currentEpoch(); current > 0 {
		p.applyStaged(block, s, params, current-1, &failures)
	}
	if err := p.updateBondedSet(block, s, params); err != nil {
		failures = append(failures, fmt.Errorf("%w: bonded set kept as it was: %w", precompiles.ErrDegraded, err))
	}
	if epoch, ok := block.ClosingEpoch.Get(); ok {
		if err := snapshotEpoch(block, s, params, epoch); err != nil {
			failures = append(failures, fmt.Errorf("%w: epoch %d's bonded set not snapshotted: %w", precompiles.ErrDegraded, epoch, err))
		}
	}
	drain(s.validatorQueue(), block.UnixNano, "validator", &failures,
		func(common.Hash) uint64 { return 1 },
		func(e common.Hash) error { return p.completeValidatorUnbonding(block, s, e) }, nil)
	drain(s.unbondingQueue(), block.UnixNano, "unbonding", &failures,
		func(e common.Hash) uint64 {
			delegator, op := unbondingQueueEntry(s, e)
			return 1 + s.unbondingEntryCount(delegator, op)
		},
		func(e common.Hash) error { return completeUnbonding(block, s, e) }, nil)
	drain(s.redelegationQueue(), block.UnixNano, "redelegation", &failures,
		func(common.Hash) uint64 { return 1 },
		func(e common.Hash) error { return completeRedelegation(s, e) }, nil)
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

// epochSnapshotsKept is how many epoch snapshots are kept, as the committee
// serving an epoch is the bonded set at the end of the epoch two before it.
const epochSnapshotsKept = 3

// projectedBondedSet returns the bonded set as it will be once the staged
// operations queued now take effect, as many as one EndBlock applies: the set an
// update would bond after applying them, or the set as it is when that update
// fails. It leaves the state as it was and calls no hooks.
func projectedBondedSet(block precompiles.BlockContext, s store, params chainparams.Staking) ([]BondedValidator, error) {
	snap := s.State.Snapshot()
	defer s.State.RevertToSnapshot(snap)
	// Nothing here takes effect, so the precompiles that track delegations are
	// left out. A dropped operation is reported when it is applied for real.
	var projection Precompile
	var dropped []error
	projection.applyStaged(block, s, params, math.MaxUint64, &dropped)
	// A failed update keeps the bonded set as it was, as the real one would.
	_ = projection.updateBondedSet(block, s, params)
	return bondedValidators(s)
}

// snapshotEpoch records the projected bonded set, with each member's consensus
// key, as epoch's snapshot and deletes the one that no longer falls among the
// latest epochSnapshotsKept.
func snapshotEpoch(block precompiles.BlockContext, s store, params chainparams.Staking, epoch uint64) error {
	set, err := projectedBondedSet(block, s, params)
	if err != nil {
		return err
	}
	// The keys are copied rather than looked up when the snapshot is read,
	// because a member can be removed before its snapshot is retired.
	s.setEpochValidators(epoch, set)
	if epoch >= epochSnapshotsKept {
		s.setEpochValidators(epoch-epochSnapshotsKept, nil)
	}
	s.setLatestSnapshotEpoch(epoch)
	return nil
}

// drain completes each entry of q due by now, in queue order, under a budget of
// its own, until none is due or the next does not fit what is left. Each entry
// is removed from q once completed. A completion that fails is reverted and
// reported, fail, when given, records the outcome, and the entry is removed all
// the same, so it holds up no other. A queue whose head is past its tail is
// reported and left alone.
func drain(q queue, now uint64, name string, failures *[]error, cost func(e common.Hash) uint64, complete, fail func(e common.Hash) error) {
	if err := q.check(); err != nil {
		*failures = append(*failures, fmt.Errorf("%w: %s queue: %w", precompiles.ErrDegraded, name, err))
		return
	}
	budget := uint64(endBlockBudget)
	for e, ok := q.due(now); ok; e, ok = q.due(now) {
		if !take(&budget, cost(e)) {
			return
		}
		snap := q.s.State.Snapshot()
		if err := complete(e); err != nil {
			q.s.State.RevertToSnapshot(snap)
			*failures = append(*failures, fmt.Errorf("%w: %s queue entry %d dropped: %w", precompiles.ErrDegraded, name, q.head(), err))
			if fail != nil {
				failInPlace(q, name, e, fail, failures)
			}
		}
		q.pop()
	}
}

// failInPlace runs fail on entry e of q, whose completion failed, reverting it
// and reporting why when it fails too.
func failInPlace(q queue, name string, e common.Hash, fail func(e common.Hash) error, failures *[]error) {
	snap := q.s.State.Snapshot()
	if err := fail(e); err != nil {
		q.s.State.RevertToSnapshot(snap)
		*failures = append(*failures, fmt.Errorf("%w: %s queue entry %d left unsettled: %w", precompiles.ErrDegraded, name, q.head(), err))
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
