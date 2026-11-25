package staking

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// Kinds of staged operation, numbered as StakingStorage.StagedOperation's kind.
const (
	stagedDelegate   uint8 = 1
	stagedUndelegate uint8 = 2
)

var (
	errUnknownStagedKind = errors.New("unknown staged operation kind")
	errStagedNotCounted  = errors.New("staged operation is not counted in its delegation's staged totals")
)

// stage queues op to take effect at the end of the epoch after the current one,
// counts it in its delegations' staged totals, and returns that epoch.
func stage(s store, op stagedOperation) (uint64, error) {
	q := s.stagedQueue()
	op.epoch = q.clamp(s.currentEpoch() + 1)
	e, err := q.push(op.epoch)
	if err != nil {
		return 0, err
	}
	s.setStagedOperation(e, op)
	countStaged(s, op)
	return op.epoch, nil
}

// countStaged adds op to its staged totals: a delegation to the count of those
// staged to its validator, and an undelegation to its delegation's shares
// staged to leave and count of undelegations staged.
func countStaged(s store, op stagedOperation) {
	switch op.kind {
	case stagedDelegate:
		s.setStagedInbound(op.validator, s.stagedInbound(op.validator)+1)
	case stagedUndelegate:
		staged := s.stagedDelegation(op.delegator, op.validator)
		staged.sharesOut.Add(staged.sharesOut, op.amount)
		staged.undelegations++
		s.setStagedDelegation(op.delegator, op.validator, staged)
	}
}

// uncountStaged takes op out of the staged totals countStaged added it to,
// failing when they do not hold it.
func uncountStaged(s store, op stagedOperation) error {
	switch op.kind {
	case stagedDelegate:
		inbound := s.stagedInbound(op.validator)
		if inbound == 0 {
			return errStagedNotCounted
		}
		s.setStagedInbound(op.validator, inbound-1)
	case stagedUndelegate:
		staged := s.stagedDelegation(op.delegator, op.validator)
		if staged.sharesOut.Cmp(op.amount) < 0 || staged.undelegations == 0 {
			return errStagedNotCounted
		}
		staged.sharesOut.Sub(staged.sharesOut, op.amount)
		staged.undelegations--
		s.setStagedDelegation(op.delegator, op.validator, staged)
	}
	return nil
}

// applyStaged applies, in the order they were staged, every staged operation
// due by the end of epoch ended. One that fails is dropped: a delegation's
// tokens go back to its delegator, and an undelegation's stake stays where it
// is.
func (p Precompile) applyStaged(block precompiles.BlockContext, s store, params chainparams.Staking, ended uint64, failures *[]error) {
	// The bonded set's power as the operations applied so far leave it. The
	// stored power is only updated once they all are, and a delegation checked
	// against it would not see the stake an earlier one moved.
	bondedPower := s.lastTotalPower()
	// No budget: the snapshot the previous epoch's end took counted every one.
	drain(s.stagedQueue(), ended, "staged", failures, nil,
		func(e common.Hash) error {
			return p.applyStagedOperation(block, s, params, bondedPower, s.stagedOperationAt(e))
		},
		func(e common.Hash) error { return p.dropStaged(block, s, s.stagedOperationAt(e), failures) })
}

// applyStagedOperation takes op out of its delegations' staged totals, carries
// it out as the call that staged it would have at this block, and adds what op
// changed the bonded set's power by to bondedPower.
func (p Precompile) applyStagedOperation(block precompiles.BlockContext, s store, params chainparams.Staking, bondedPower *big.Int, op stagedOperation) error {
	if err := uncountStaged(s, op); err != nil {
		return err
	}
	before := bondedPowerOf(s, op.validator)
	if err := p.carryOutStaged(block, s, params, bondedPower, op); err != nil {
		return err
	}
	bondedPower.Add(bondedPower, new(big.Int).Sub(bondedPowerOf(s, op.validator), before))
	return nil
}

// carryOutStaged carries out op as the call that staged it would have at this
// block, checking the voting power ratio against bondedPower.
func (p Precompile) carryOutStaged(block precompiles.BlockContext, s store, params chainparams.Staking, bondedPower *big.Int, op stagedOperation) error {
	switch op.kind {
	case stagedDelegate:
		v, ok := s.validator(op.validator)
		if !ok {
			return fmt.Errorf("%w: %s", errNoValidatorFound, op.validator)
		}
		_, err := p.delegate(block, s, params, bondedPower, op.delegator, op.validator, &v, op.amount, StagedEscrow)
		return err
	case stagedUndelegate:
		_, _, err := p.undelegate(block, s, params, op.delegator, op.validator, op.amount)
		return err
	}
	return fmt.Errorf("%w: %d", errUnknownStagedKind, op.kind)
}

// bondedPowerOf returns op's consensus power when its validator is bonded, and
// zero otherwise.
func bondedPowerOf(s store, op common.Address) *big.Int {
	if v, ok := s.validator(op); ok && v.bonded() {
		return consensusPower(v.tokens)
	}
	return new(big.Int)
}

// dropStaged takes op out of its staged totals and, for a delegation, returns
// its tokens to its delegator, then removes its validator when that leaves it
// vacant. A removal that fails is reported in failures and keeps the validator,
// and the refund stands.
func (p Precompile) dropStaged(block precompiles.BlockContext, s store, op stagedOperation, failures *[]error) error {
	if err := uncountStaged(s, op); err != nil {
		return err
	}
	if op.kind != stagedDelegate {
		return nil
	}
	if err := transfer(s.State, StagedEscrow, op.delegator, op.amount); err != nil {
		return err
	}
	// The entry leaves the queue whatever happens here, and nothing else would
	// return its tokens, so a failed removal must not take the refund back.
	p.removeIfVacantReporting(block, s, op.validator, failures)
	return nil
}
