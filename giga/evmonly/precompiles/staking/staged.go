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
	stagedRedelegate uint8 = 3
)

var (
	errUnknownStagedKind = errors.New("unknown staged operation kind")
	errStagedNotCounted  = errors.New("staged operation is not counted in its delegation's staged totals")
	errUnmetered         = errors.New("staking call has no gas meter")
)

// The gas staging charges for the work it adds to an epoch's end, beyond the
// call's own storage: none for the first stagedFreeWork units staged during an
// epoch, then stagedSurchargeBase a unit, doubling every stagedSurchargeDoubling
// units, up to a quarter of the block's gas limit.
const (
	stagedFreeWork          = 10_000
	stagedSurchargeBase     = 100_000
	stagedSurchargeDoubling = 2_000
)

// stage charges the call stagingGas for op's work, queues op to take effect at
// the end of the epoch after the current one, counts it in its delegations'
// staged totals, and returns that epoch.
func stage(ctx *precompiles.Context, s store, op stagedOperation) (uint64, error) {
	if err := chargeStaging(ctx, s, op.work()); err != nil {
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

// chargeStaging charges the call stagingGas for units of work staged after the
// work already staged this epoch, and adds them to it.
func chargeStaging(ctx *precompiles.Context, s store, units uint64) error {
	if ctx.Gas == nil {
		return errUnmetered
	}
	staged := s.stagedWork()
	if !ctx.Gas.UseGas(stagingGas(staged, units, ctx.Block.GasLimit)) {
		return vm.ErrOutOfGas
	}
	s.setStagedWork(staged + units)
	return nil
}

// stagingGas returns the gas units of work pay when staged units were staged
// before them this epoch, in a block whose gas limit is gasLimit.
func stagingGas(staged, units, gasLimit uint64) uint64 {
	// A unit pays at most a quarter of the block, so an operation of two units
	// leaves room in its block for the call's own gas.
	ceiling := gasLimit / 4
	var gas uint64
	for i := range units {
		gas += unitGas(staged+i, ceiling)
	}
	return gas
}

// unitGas returns the gas the unit of work staged after staged others pays:
// none within the free tier, then stagedSurchargeBase doubling every
// stagedSurchargeDoubling units, up to ceiling.
func unitGas(staged, ceiling uint64) uint64 {
	if staged < stagedFreeWork {
		return 0
	}
	doublings := (staged - stagedFreeWork) / stagedSurchargeDoubling
	gas := uint64(stagedSurchargeBase)
	for i := uint64(0); i < doublings && gas < ceiling; i++ {
		gas *= 2
	}
	return min(gas, ceiling)
}

// countStaged adds op to the staged totals of the delegations it changes, the
// shares an undelegation or redelegation takes from its source and how many of
// each it stages, and of the validator it moves stake to.
func countStaged(s store, op stagedOperation) {
	if to, ok := op.inbound(); ok {
		s.setStagedInbound(to, s.stagedInbound(to)+1)
	}
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
	to, inbound := op.inbound()
	if inbound && s.stagedInbound(to) == 0 {
		return errStagedNotCounted
	}
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
	if inbound {
		s.setStagedInbound(to, s.stagedInbound(to)-1)
	}
	return nil
}

// applyStaged applies, in the order they were staged, every staged operation
// due by the end of epoch ended. One that fails is dropped: a delegation's
// tokens go back to its delegator, and the stake an undelegation or
// redelegation would have moved stays where it is.
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
	before := bondedPowerOf(s, op.validators())
	if err := p.carryOutStaged(block, s, params, bondedPower, op); err != nil {
		return err
	}
	bondedPower.Add(bondedPower, new(big.Int).Sub(bondedPowerOf(s, op.validators()), before))
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
		_, err := p.delegate(block, s, params, bondedPower, op.delegator, op.validator, &v, op.amount, v.status, StagedEscrow)
		return err
	case stagedUndelegate:
		_, _, err := p.undelegate(block, s, params, op.delegator, op.validator, op.amount)
		return err
	case stagedRedelegate:
		_, _, err := p.redelegate(block, s, params, bondedPower, op.delegator, op.validator, op.dstValidator, op.amount)
		return err
	}
	return fmt.Errorf("%w: %d", errUnknownStagedKind, op.kind)
}

// inbound returns the validator op moves stake to, if any.
func (op stagedOperation) inbound() (common.Address, bool) {
	switch op.kind {
	case stagedDelegate:
		return op.validator, true
	case stagedRedelegate:
		return op.dstValidator, true
	}
	return common.Address{}, false
}

// work returns the units of work op adds to an epoch's end: one per delegation
// it changes.
func (op stagedOperation) work() uint64 {
	if op.kind == stagedRedelegate {
		return 2
	}
	return 1
}

// validators returns the validators op moves stake to or from.
func (op stagedOperation) validators() []common.Address {
	if op.kind == stagedRedelegate {
		return []common.Address{op.validator, op.dstValidator}
	}
	return []common.Address{op.validator}
}

// bondedPowerOf returns the consensus power ops hold among the bonded
// validators.
func bondedPowerOf(s store, ops []common.Address) *big.Int {
	power := new(big.Int)
	for _, op := range ops {
		if v, ok := s.validator(op); ok && v.bonded() {
			power.Add(power, consensusPower(v.tokens))
		}
	}
	return power
}

// dropStaged takes op out of its staged totals, returns a delegation's tokens to
// its delegator, and removes the validator op would have moved stake to when
// that leaves it vacant.
func (p Precompile) dropStaged(block precompiles.BlockContext, s store, op stagedOperation) error {
	if err := uncountStaged(s, op); err != nil {
		return err
	}
	if op.kind == stagedDelegate {
		if err := transfer(s.State, StagedEscrow, op.delegator, op.amount); err != nil {
			return err
		}
	}
	to, ok := op.inbound()
	if !ok {
		return nil
	}
	v, ok := s.validator(to)
	if !ok {
		return nil
	}
	return p.removeIfVacant(block, s, to, v)
}
