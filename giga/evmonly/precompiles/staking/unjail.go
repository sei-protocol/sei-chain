package staking

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	errMissingSelfDelegation = errors.New("validator has no self-delegation; cannot be unjailed")
	errSelfDelegationTooLow  = errors.New("validator's self delegation less than minimum; cannot be unjailed")
	errValidatorNotJailed    = errors.New("validator not jailed; cannot be unjailed")
)

// Unjail unjails op's validator and returns it to the power index, as
// x/slashing's Unjail does. It fails unless the validator is jailed and op's
// own delegation to it is worth at least its minimum self-delegation.
func Unjail(state precompiles.State, op common.Address) error {
	s := newStore(state)
	v, ok := s.validator(op)
	if !ok {
		return fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	shares, ok := s.delegation(op, op)
	if !ok {
		return errMissingSelfDelegation
	}
	if self := truncateDec(v.tokensFromShares(shares)); self.Cmp(v.minSelfDelegation) < 0 {
		return fmt.Errorf("%w: %s less than %s", errSelfDelegationTooLow, self, v.minSelfDelegation)
	}
	if !v.jailed {
		return errValidatorNotJailed
	}
	// x/slashing also refuses a tombstoned validator and one whose jail time has
	// not passed. Without signing info, the only jail is a self-delegation below
	// its minimum, which x/slashing lets a validator leave at once.
	s.unjail(op, &v)
	return nil
}
