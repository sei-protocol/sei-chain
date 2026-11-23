package staking

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// Kinds of staged operation, numbered as StakingStorage.StagedOperation's kind.
const (
	stagedDelegate   uint8 = 1
	stagedUndelegate uint8 = 2
	stagedRedelegate uint8 = 3
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

// countStaged adds op to the staged totals of the delegations it changes: the
// shares an undelegation or redelegation takes from its source, and how many of
// each it stages.
func countStaged(s store, op stagedOperation) {
	switch op.kind {
	case stagedUndelegate:
		staged := s.stagedDelegation(op.delegator, op.validator)
		staged.sharesOut.Add(staged.sharesOut, op.amount)
		staged.undelegations++
		s.setStagedDelegation(op.delegator, op.validator, staged)
	case stagedRedelegate:
		staged := s.stagedDelegation(op.delegator, op.validator)
		staged.sharesOut.Add(staged.sharesOut, op.amount)
		staged.redelegationsOut++
		s.setStagedDelegation(op.delegator, op.validator, staged)
		staged = s.stagedDelegation(op.delegator, op.dstValidator)
		staged.redelegationsIn++
		s.setStagedDelegation(op.delegator, op.dstValidator, staged)
	}
}

// uncountStaged takes op out of the staged totals countStaged added it to,
// failing when they do not hold it.
func uncountStaged(s store, op stagedOperation) error {
	switch op.kind {
	case stagedUndelegate:
		staged := s.stagedDelegation(op.delegator, op.validator)
		if staged.sharesOut.Cmp(op.amount) < 0 || staged.undelegations == 0 {
			return errStagedNotCounted
		}
		staged.sharesOut.Sub(staged.sharesOut, op.amount)
		staged.undelegations--
		s.setStagedDelegation(op.delegator, op.validator, staged)
	case stagedRedelegate:
		src, dst := s.stagedDelegation(op.delegator, op.validator), s.stagedDelegation(op.delegator, op.dstValidator)
		if src.sharesOut.Cmp(op.amount) < 0 || src.redelegationsOut == 0 || dst.redelegationsIn == 0 {
			return errStagedNotCounted
		}
		src.sharesOut.Sub(src.sharesOut, op.amount)
		src.redelegationsOut--
		dst.redelegationsIn--
		s.setStagedDelegation(op.delegator, op.validator, src)
		s.setStagedDelegation(op.delegator, op.dstValidator, dst)
	}
	return nil
}

// applyStaged applies, in the order they were staged, the staged operations
// due by the end of epoch ended. One that fails is dropped: a delegation's
// tokens go back to its delegator, and the stake an undelegation or
// redelegation would have moved stays where it is.
func (p Precompile) applyStaged(block precompiles.BlockContext, s store, params chainparams.Staking, ended uint64, failures *[]error) {
	drain(s.stagedQueue(), ended, "staged", failures,
		func(common.Hash) uint64 { return 1 },
		func(e common.Hash) error { return p.applyStagedOperation(block, s, params, s.stagedOperationAt(e)) },
		func(e common.Hash) error { return dropStaged(s, s.stagedOperationAt(e)) })
}

// applyStagedOperation takes op out of its delegations' staged totals and
// carries it out as the call that staged it would have at this block.
func (p Precompile) applyStagedOperation(block precompiles.BlockContext, s store, params chainparams.Staking, op stagedOperation) error {
	if err := uncountStaged(s, op); err != nil {
		return err
	}
	switch op.kind {
	case stagedDelegate:
		v, ok := s.validator(op.validator)
		if !ok {
			return fmt.Errorf("%w: %s", errNoValidatorFound, op.validator)
		}
		_, err := p.delegate(block, s, params, op.delegator, op.validator, &v, op.amount, v.status, StagedEscrow)
		return err
	case stagedUndelegate:
		_, _, err := p.undelegate(block, s, params, op.delegator, op.validator, op.amount)
		return err
	case stagedRedelegate:
		_, _, err := p.redelegate(block, s, params, op.delegator, op.validator, op.dstValidator, op.amount)
		return err
	}
	return fmt.Errorf("%w: %d", errUnknownStagedKind, op.kind)
}

// dropStaged takes op out of its delegations' staged totals and, for a
// delegation, returns its tokens to its delegator.
func dropStaged(s store, op stagedOperation) error {
	if err := uncountStaged(s, op); err != nil {
		return err
	}
	if op.kind == stagedDelegate {
		return transfer(s.State, StagedEscrow, op.delegator, op.amount)
	}
	return nil
}
