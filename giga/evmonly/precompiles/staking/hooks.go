package staking

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Hooks are the calls staking makes into the precompiles that track its
// delegations and validators, at the points x/staking's StakingHooks are
// called. A hook must not fail: an error it returns, or a state access it
// fails, fails the block that called it, which halts the chain. At genesis, its
// error fails the genesis write.
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

// hooks returns p's Hooks, or NoHooks when it has none. When p records hook
// errors, each hook runs as recordingHooks runs it.
func (p Precompile) hooks() Hooks {
	var hooks Hooks = NoHooks{}
	if p.Hooks != nil {
		hooks = p.Hooks
	}
	if p.hookErr == nil {
		return hooks
	}
	return recordingHooks{hooks: hooks, err: p.hookErr}
}

// recordingHookErrors returns p keeping the first error its hooks return in
// err.
func (p Precompile) recordingHookErrors(err *error) Precompile {
	p.hookErr = err
	return p
}

// recordingHooks runs hooks, keeping the first error one returns in err. The
// error is also returned, so the operation that called the hook stops there.
type recordingHooks struct {
	hooks Hooks
	err   *error
}

var _ Hooks = recordingHooks{}

// record keeps err, the error the hook named name returned, unless an earlier
// hook's is kept, and returns it.
func (r recordingHooks) record(name string, err error) error {
	if err != nil && *r.err == nil {
		*r.err = fmt.Errorf("%s hook: %w", name, err)
	}
	return err
}

func (r recordingHooks) AfterValidatorCreated(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.record("AfterValidatorCreated", r.hooks.AfterValidatorCreated(block, state, validator))
}

func (r recordingHooks) BeforeDelegationCreated(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.record("BeforeDelegationCreated", r.hooks.BeforeDelegationCreated(block, state, delegator, validator))
}

func (r recordingHooks) BeforeDelegationSharesModified(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.record("BeforeDelegationSharesModified", r.hooks.BeforeDelegationSharesModified(block, state, delegator, validator))
}

func (r recordingHooks) BeforeDelegationRemoved(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.record("BeforeDelegationRemoved", r.hooks.BeforeDelegationRemoved(block, state, delegator, validator))
}

func (r recordingHooks) AfterDelegationModified(block precompiles.BlockContext, state precompiles.State, delegator, validator common.Address) error {
	return r.record("AfterDelegationModified", r.hooks.AfterDelegationModified(block, state, delegator, validator))
}

func (r recordingHooks) AfterValidatorBonded(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.record("AfterValidatorBonded", r.hooks.AfterValidatorBonded(block, state, validator))
}

func (r recordingHooks) AfterValidatorBeginUnbonding(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.record("AfterValidatorBeginUnbonding", r.hooks.AfterValidatorBeginUnbonding(block, state, validator))
}

func (r recordingHooks) AfterValidatorRemoved(block precompiles.BlockContext, state precompiles.State, validator common.Address) error {
	return r.record("AfterValidatorRemoved", r.hooks.AfterValidatorRemoved(block, state, validator))
}
