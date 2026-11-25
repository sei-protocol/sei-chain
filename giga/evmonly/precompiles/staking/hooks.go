package staking

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Hooks are the calls staking makes into the precompiles that track its
// delegations and validators, at the points x/staking's StakingHooks are
// called. A hook must not fail. In EndBlock, one that does has its writes
// reverted and its failure reported, and the operation that called it goes
// ahead; at genesis, its error fails the genesis write.
type Hooks interface {
	AfterValidatorCreated(block precompiles.BlockContext, state precompiles.State, validator common.Address) error
	BeforeDelegationCreated(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error
	BeforeDelegationSharesModified(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error
	BeforeDelegationRemoved(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error
	AfterDelegationModified(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error
	AfterValidatorBonded(block precompiles.BlockContext, state precompiles.State, validator common.Address) error
	AfterValidatorBeginUnbonding(block precompiles.BlockContext, state precompiles.State, validator common.Address) error
	AfterValidatorRemoved(block precompiles.BlockContext, state precompiles.State, validator common.Address) error
}

// NoHooks is Hooks that does nothing.
type NoHooks struct{}

var _ Hooks = NoHooks{}

func (NoHooks) AfterValidatorCreated(precompiles.BlockContext, precompiles.State, common.Address) error {
	return nil
}

func (NoHooks) BeforeDelegationCreated(precompiles.BlockContext, precompiles.State, common.Address, common.Address) error {
	return nil
}

func (NoHooks) BeforeDelegationSharesModified(precompiles.BlockContext, precompiles.State, common.Address, common.Address) error {
	return nil
}

func (NoHooks) BeforeDelegationRemoved(precompiles.BlockContext, precompiles.State, common.Address, common.Address) error {
	return nil
}

func (NoHooks) AfterDelegationModified(precompiles.BlockContext, precompiles.State, common.Address, common.Address) error {
	return nil
}

func (NoHooks) AfterValidatorBonded(precompiles.BlockContext, precompiles.State, common.Address) error {
	return nil
}

func (NoHooks) AfterValidatorBeginUnbonding(precompiles.BlockContext, precompiles.State, common.Address) error {
	return nil
}

func (NoHooks) AfterValidatorRemoved(precompiles.BlockContext, precompiles.State, common.Address) error {
	return nil
}

// hooks returns p's Hooks, or NoHooks when it has none. When p reports hook
// failures, each hook runs as reportingHooks runs it.
func (p Precompile) hooks() Hooks {
	var hooks Hooks = NoHooks{}
	if p.Hooks != nil {
		hooks = p.Hooks
	}
	if p.hookFailures == nil {
		return hooks
	}
	return reportingHooks{hooks: hooks, failures: p.hookFailures}
}

// withHookFailures returns p reporting its hooks' failures in failures in place
// of returning them.
func (p Precompile) withHookFailures(failures *[]error) Precompile {
	p.hookFailures = failures
	return p
}

// reportingHooks runs each of hooks in its own snapshot. A hook that fails has
// its writes reverted and its failure added to failures, and returns nil.
type reportingHooks struct {
	hooks    Hooks
	failures *[]error
}

var _ Hooks = reportingHooks{}

// run runs hook, the hook named name, reverting its writes to state and
// reporting why when it fails.
func (r reportingHooks) run(state precompiles.State, name string, hook func() error) error {
	snap := state.Snapshot()
	if err := hook(); err != nil {
		state.RevertToSnapshot(snap)
		*r.failures = append(*r.failures, fmt.Errorf("%w: %s hook reverted: %w", precompiles.ErrDegraded, name, err))
	}
	return nil
}

func (r reportingHooks) AfterValidatorCreated(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.run(state, "AfterValidatorCreated", func() error { return r.hooks.AfterValidatorCreated(block, state, validator) })
}

func (r reportingHooks) BeforeDelegationCreated(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.run(state, "BeforeDelegationCreated", func() error { return r.hooks.BeforeDelegationCreated(block, state, delegator, validator) })
}

func (r reportingHooks) BeforeDelegationSharesModified(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.run(state, "BeforeDelegationSharesModified", func() error { return r.hooks.BeforeDelegationSharesModified(block, state, delegator, validator) })
}

func (r reportingHooks) BeforeDelegationRemoved(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.run(state, "BeforeDelegationRemoved", func() error { return r.hooks.BeforeDelegationRemoved(block, state, delegator, validator) })
}

func (r reportingHooks) AfterDelegationModified(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.run(state, "AfterDelegationModified", func() error { return r.hooks.AfterDelegationModified(block, state, delegator, validator) })
}

func (r reportingHooks) AfterValidatorBonded(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.run(state, "AfterValidatorBonded", func() error { return r.hooks.AfterValidatorBonded(block, state, validator) })
}

func (r reportingHooks) AfterValidatorBeginUnbonding(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.run(state, "AfterValidatorBeginUnbonding", func() error { return r.hooks.AfterValidatorBeginUnbonding(block, state, validator) })
}

func (r reportingHooks) AfterValidatorRemoved(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.run(state, "AfterValidatorRemoved", func() error { return r.hooks.AfterValidatorRemoved(block, state, validator) })
}
