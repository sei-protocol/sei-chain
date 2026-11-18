package staking

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Hooks are the calls staking makes into the precompiles that track its
// delegations and validators, at the points x/staking's StakingHooks are
// called. Each runs under the rules of the Run or EndBlock that calls it, and
// an error fails the operation that called it.
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

// hooks returns p's Hooks, or NoHooks when it has none.
func (p Precompile) hooks() Hooks {
	if p.Hooks == nil {
		return NoHooks{}
	}
	return p.Hooks
}
