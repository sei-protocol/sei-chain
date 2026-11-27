package distribution

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

// Hooks keeps distribution's records in step with staking's validators and
// delegations, as x/distribution's staking hooks do. A delegation's rewards
// are paid to its withdraw address before staking changes its shares.
type Hooks struct{}

var _ staking.Hooks = Hooks{}

func (Hooks) AfterValidatorCreated(_ precompiles.BlockContext, state precompiles.State, val common.Address) error {
	newStore(state).initializeValidator(val)
	return nil
}

func (Hooks) BeforeDelegationCreated(_ precompiles.BlockContext, state precompiles.State, _, val common.Address) error {
	s := newStore(state)
	v, err := validatorStake(s, val)
	if err != nil {
		return err
	}
	_, err = s.incrementValidatorPeriod(val, v.Tokens)
	return err
}

func (Hooks) BeforeDelegationSharesModified(block precompiles.BlockContext, state precompiles.State, delegator, val common.Address) error {
	_, err := newStore(state).withdrawDelegationRewards(block.Number, val, delegator)
	return err
}

func (Hooks) BeforeDelegationRemoved(precompiles.BlockContext, precompiles.State, common.Address, common.Address) error {
	return nil
}

func (Hooks) AfterDelegationModified(block precompiles.BlockContext, state precompiles.State, delegator, val common.Address) error {
	return newStore(state).initializeDelegation(block.Number, val, delegator)
}

func (Hooks) AfterValidatorBonded(precompiles.BlockContext, precompiles.State, common.Address) error {
	return nil
}

func (Hooks) AfterValidatorBeginUnbonding(precompiles.BlockContext, precompiles.State, common.Address) error {
	return nil
}

func (Hooks) AfterValidatorRemoved(_ precompiles.BlockContext, state precompiles.State, val common.Address) error {
	return newStore(state).removeValidator(val)
}
