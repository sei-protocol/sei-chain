package staking

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// ValidatorStake is a validator's stake and commission rate.
type ValidatorStake struct {
	Tokens          *big.Int // wei
	DelegatorShares *big.Int // 1e18-scaled
	CommissionRate  *big.Int // 1e18-scaled
}

// ReadValidatorStake returns op's validator's stake and commission rate,
// reporting whether op has a validator.
func ReadValidatorStake(state precompiles.State, op common.Address) (ValidatorStake, bool) {
	s := newStore(state)
	v, ok := s.validator(op)
	if !ok {
		return ValidatorStake{}, false
	}
	return ValidatorStake{
		Tokens:          v.tokens,
		DelegatorShares: v.delegatorShares,
		CommissionRate:  s.Uint256(at(validatorBase(op), validatorCommissionRate)),
	}, true
}

// DelegationTokens returns the wei delegator's shares in op's validator are
// worth at its current exchange rate, truncated, reporting whether delegator
// delegates to op.
func DelegationTokens(state precompiles.State, delegator, op common.Address) (*big.Int, bool) {
	s := newStore(state)
	shares, ok := s.delegation(delegator, op)
	if !ok {
		return nil, false
	}
	v, ok := s.validator(op)
	if !ok {
		return nil, false
	}
	return dec.Truncate(v.tokensFromShares(shares)), true
}

// DelegatorValidators returns the operators of the validators delegator
// delegates to.
func DelegatorValidators(state precompiles.State, delegator common.Address) []common.Address {
	set := newStore(state).delegatedValidators(delegator)
	var out []common.Address
	for i, n := uint64(0), set.Len(); i < n && !state.Failed(); i++ {
		out = append(out, solstorage.MemberAddress(set.At(i)))
	}
	return out
}
