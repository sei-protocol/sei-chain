package staking

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

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
	errUnmetered         = errors.New("staking call has no gas meter")
)

// The gas staging charges for the work it adds to an epoch's end, beyond the
// call's own storage: none for the first stagedFree operations staged during an
// epoch, then stagedSurchargeBase an operation, doubling every
// stagedSurchargeDoubling operations.
const (
	stagedFree              = 10_000
	stagedSurchargeBase     = 100_000
	stagedSurchargeDoubling = 2_000
)

// stage charges the call stagingGas, queues op to take effect at the end of the
// epoch after the current one, counts it in its staged totals, and returns that
// epoch.
func stage(ctx *precompiles.Context, s store, op stagedOperation) (uint64, error) {
	if err := chargeStaging(ctx, s); err != nil {
		return 0, err
	}
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

// chargeStaging charges the call stagingGas for one more operation staged
// this epoch, and counts it.
func chargeStaging(ctx *precompiles.Context, s store) error {
	if ctx.Gas == nil {
		return errUnmetered
	}
	staged := s.stagedThisEpoch()
	if !ctx.Gas.UseGas(stagingGas(staged, ctx.Block.GasLimit)) {
		return vm.ErrOutOfGas
	}
	s.setStagedThisEpoch(staged + 1)
	return nil
}

// stagingGas returns the gas an operation pays when staged others were staged
// before it this epoch, in a block whose gas limit is gasLimit: none within the
// free tier, then stagedSurchargeBase doubling every stagedSurchargeDoubling
// operations, up to a quarter of gasLimit.
func stagingGas(staged, gasLimit uint64) uint64 {
	if staged < stagedFree {
		return 0
	}
	// A quarter of the block leaves room for the call's own gas, and for two
	// such calls in the half of a block the base fee targets.
	ceiling := gasLimit / 4
	doublings := (staged - stagedFree) / stagedSurchargeDoubling
	gas := uint64(stagedSurchargeBase)
	for i := uint64(0); i < doublings && gas < ceiling; i++ {
		gas *= 2
	}
	return min(gas, ceiling)
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
	// No budget: the calls that staged these operations paid stagingGas for
	// this work.
	drain(s.stagedQueue(), ended, "staged", failures, nil,
		func(e common.Hash) error {
			return p.applyStagedOperation(block, s, params, bondedPower, s.stagedOperationAt(e))
		},
		func(e common.Hash) error { return p.dropStaged(block, s, s.stagedOperationAt(e)) })
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
// its tokens to its delegator and removes its validator when that leaves it
// vacant.
func (p Precompile) dropStaged(block precompiles.BlockContext, s store, op stagedOperation) error {
	if err := uncountStaged(s, op); err != nil {
		return err
	}
	if op.kind != stagedDelegate {
		return nil
	}
	if err := transfer(s.State, StagedEscrow, op.delegator, op.amount); err != nil {
		return err
	}
	v, ok := s.validator(op.validator)
	if !ok {
		return nil
	}
	return p.removeIfVacant(block, s, op.validator, v)
}
