package gov

import (
	"bytes"
	"maps"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Validator is a bonded validator as a tally weighs it. A voter whose address
// is Operator votes as the validator.
type Validator struct {
	Operator        common.Address
	BondedTokens    *big.Int
	DelegatorShares *big.Int
}

// Delegation is a delegator's shares in one validator.
type Delegation struct {
	Validator common.Address
	Shares    *big.Int
}

// Electorate is the voting power a tally weighs votes by, in the shape x/staking
// gives it to x/gov. Tallies are in the unit of its tokens.
//
// TODO(giga staking): add an Electorate reading StakingStorage at 0x1005, from
// bondedValidatorSet, validators[operator].tokens and .delegatorShares,
// delegationsByDelegator[voter] and delegations[voter][validator].shares, with
// TotalBonded the bonded escrow's balance. It replaces GenesisElectorate.
type Electorate interface {
	// BondedValidators returns every bonded validator, in a deterministic order.
	BondedValidators(precompiles.State) []Validator
	// Delegations returns delegator's delegations, in a deterministic order.
	Delegations(state precompiles.State, delegator common.Address) []Delegation
	// TotalBonded returns the tokens bonded across all validators.
	TotalBonded(precompiles.State) *big.Int
}

// GenesisElectorate is a fixed electorate from genesis. Each voter is a
// validator bonded with its weight, and nothing is delegated.
type GenesisElectorate map[common.Address]*big.Int

var _ Electorate = GenesisElectorate(nil)

func (e GenesisElectorate) BondedValidators(precompiles.State) []Validator {
	operators := slices.SortedFunc(maps.Keys(e), func(a, b common.Address) int {
		return bytes.Compare(a[:], b[:])
	})
	validators := make([]Validator, 0, len(operators))
	for _, op := range operators {
		if e[op].Sign() <= 0 {
			continue
		}
		validators = append(validators, Validator{Operator: op, BondedTokens: e[op], DelegatorShares: e[op]})
	}
	return validators
}

func (GenesisElectorate) Delegations(precompiles.State, common.Address) []Delegation {
	return nil
}

func (e GenesisElectorate) TotalBonded(precompiles.State) *big.Int {
	total := new(big.Int)
	for _, weight := range e {
		if weight.Sign() > 0 {
			total.Add(total, weight)
		}
	}
	return total
}
