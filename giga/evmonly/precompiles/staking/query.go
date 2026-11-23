package staking

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var errNoUnbondingDelegation = errors.New("no unbonding delegation found")

func delegationQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator, op := args[0].(common.Address), args[1].(common.Address)
	shares, ok := s.delegation(delegator, op)
	if !ok {
		return nil, fmt.Errorf("%w: delegator %s, validator %s", errNoDelegation, delegator.Hex(), op.Hex())
	}
	v, ok := s.validator(op)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	return method.Outputs.Pack(delegationData(delegator, op, shares, v))
}

func validatorQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	return packValidator(s, method, args[0].(common.Address))
}

// validatorsQuery pages through every validator record, keeping those with the
// filtered status, so a page may hold fewer than pageLimit validators while
// still returning a next key.
func validatorsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	status := args[0].(uint8)
	set := s.validatorSet()
	start, end, nextKey, err := page(set.Len(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]Validator, 0, end-start)
	for i := start; i < end; i++ {
		op := solstorage.MemberAddress(set.At(i))
		v, ok := s.validator(op)
		if !ok || (status != 0 && v.status != status) {
			continue
		}
		out = append(out, validatorData(s, op, v))
	}
	return method.Outputs.Pack(out, nextKey)
}

// validatorDelegationsQuery pages through op's delegators. A validator with no
// record lists no delegations, as in x/staking.
func validatorDelegationsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	op := args[0].(common.Address)
	delegators := s.delegators(op)
	start, end, nextKey, err := page(delegators.Len(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	v, ok := s.validator(op)
	if !ok && end > start {
		return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	out := make([]Delegation, 0, end-start)
	for i := start; i < end; i++ {
		delegator := solstorage.MemberAddress(delegators.At(i))
		shares, _ := s.delegation(delegator, op)
		out = append(out, delegationData(delegator, op, shares, v))
	}
	return method.Outputs.Pack(out, nextKey)
}

func delegatorDelegationsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator := args[0].(common.Address)
	ops := s.delegatedValidators(delegator)
	start, end, nextKey, err := page(ops.Len(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]Delegation, 0, end-start)
	for i := start; i < end; i++ {
		op := solstorage.MemberAddress(ops.At(i))
		v, ok := s.validator(op)
		if !ok {
			return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
		}
		shares, _ := s.delegation(delegator, op)
		out = append(out, delegationData(delegator, op, shares, v))
	}
	return method.Outputs.Pack(out, nextKey)
}

func delegatorValidatorQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator, op := args[0].(common.Address), args[1].(common.Address)
	if _, ok := s.delegation(delegator, op); !ok {
		return nil, fmt.Errorf("%w: delegator %s, validator %s", errNoDelegation, delegator.Hex(), op.Hex())
	}
	return packValidator(s, method, op)
}

func delegatorValidatorsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator := args[0].(common.Address)
	ops := s.delegatedValidators(delegator)
	start, end, nextKey, err := page(ops.Len(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]Validator, 0, end-start)
	for i := start; i < end; i++ {
		op := solstorage.MemberAddress(ops.At(i))
		v, ok := s.validator(op)
		if !ok {
			return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
		}
		out = append(out, validatorData(s, op, v))
	}
	return method.Outputs.Pack(out, nextKey)
}

func unbondingDelegationQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator, op := args[0].(common.Address), args[1].(common.Address)
	entries, err := s.unbondingEntries(delegator, op)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: delegator %s, validator %s", errNoUnbondingDelegation, delegator.Hex(), op.Hex())
	}
	out := UnbondingDelegation{Delegator: delegator, Validator: op, Entries: make([]UnbondingDelegationEntry, len(entries))}
	for i, e := range entries {
		out.Entries[i] = UnbondingDelegationEntry{
			CreationHeight: e.creationHeight,
			CompletionTime: e.completionTime,
			InitialBalance: e.initialBalance,
			Balance:        e.balance,
		}
	}
	return method.Outputs.Pack(out)
}

// stagedOperationsQuery pages through the staged queue by queue index, keeping
// the operations of the filtered delegator, so a page may hold fewer than
// pageLimit operations while still returning a next key.
func stagedOperationsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator := args[0].(common.Address)
	q := s.stagedQueue()
	if err := q.check(); err != nil {
		return nil, err
	}
	start, end, nextKey, err := pageFrom(q.head(), q.tail(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]StagedOperation, 0, end-start)
	for i := start; i < end; i++ {
		op := s.stagedOperationAt(q.entry(i))
		if delegator != (common.Address{}) && op.delegator != delegator {
			continue
		}
		data, err := stagedOperationData(s, op)
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return method.Outputs.Pack(out, nextKey)
}

// stagedOperationData returns op with the tokens it stages: a delegation's
// amount, or what an undelegation's or redelegation's shares are worth at its
// source validator's current exchange rate.
func stagedOperationData(s store, op stagedOperation) (StagedOperation, error) {
	out := StagedOperation{
		Epoch:        op.epoch,
		Kind:         op.kind,
		Delegator:    op.delegator,
		Validator:    op.validator,
		DstValidator: op.dstValidator,
		Amount:       op.amount,
		Shares:       new(big.Int),
	}
	if op.kind == stagedDelegate {
		return out, nil
	}
	v, ok := s.validator(op.validator)
	if !ok {
		return StagedOperation{}, fmt.Errorf("%w: %s", errNoValidatorFound, op.validator)
	}
	out.Amount, out.Shares = v.unbondedTokens(op.amount), op.amount
	return out, nil
}

func poolQuery(s store, method *abi.Method) ([]byte, error) {
	return method.Outputs.Pack(Pool{
		NotBondedTokens: s.State.GetBalance(NotBondedEscrow),
		BondedTokens:    s.State.GetBalance(BondedEscrow),
	})
}

func paramsQuery(s store, method *abi.Method) ([]byte, error) {
	params := s.params()
	return method.Outputs.Pack(StakingParams{
		UnbondingTime:                      params.UnbondingTime,
		MaxValidators:                      params.MaxValidators,
		MaxEntries:                         params.MaxEntries,
		MaxVotingPowerRatio:                params.MaxVotingPowerRatio,
		MaxVotingPowerEnforcementThreshold: params.MaxVotingPowerEnforcementThreshold,
	})
}

// packValidator packs op's validator as method's result, failing when op has
// none.
func packValidator(s store, method *abi.Method, op common.Address) ([]byte, error) {
	v, ok := s.validator(op)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	return method.Outputs.Pack(validatorData(s, op, v))
}

// validatorData returns v, op's stored validator, with its description and
// commission. A description field that does not decode reads as empty.
func validatorData(s store, op common.Address, v validator) Validator {
	base := validatorBase(op)
	str := func(m member) string {
		value, _ := s.String(at(base, m))
		return value
	}
	return Validator{
		Operator:        op,
		ConsensusPubkey: v.consensusPubkey,
		Jailed:          v.jailed,
		Status:          v.status,
		Tokens:          v.tokens,
		DelegatorShares: v.delegatorShares,
		Description: Description{
			Moniker:         str(validatorMoniker),
			Identity:        str(validatorIdentity),
			Website:         str(validatorWebsite),
			SecurityContact: str(validatorSecurityContact),
			Details:         str(validatorDetails),
		},
		UnbondingHeight:         v.unbondingHeight,
		UnbondingTime:           v.unbondingTime,
		CommissionRate:          s.Uint256(at(base, validatorCommissionRate)),
		CommissionMaxRate:       s.Uint256(at(base, validatorCommissionMaxRate)),
		CommissionMaxChangeRate: s.Uint256(at(base, validatorCommissionMaxChange)),
		CommissionUpdateTime:    s.Uint64(at(base, validatorCommissionUpdate), validatorCommissionUpdate.offset),
		MinSelfDelegation:       v.minSelfDelegation,
	}
}

// delegationData returns delegator's delegation of shares to op, whose
// validator is v, with the tokens those shares are worth, as x/staking's
// delegation response truncates them.
func delegationData(delegator, op common.Address, shares *big.Int, v validator) Delegation {
	return Delegation{
		Delegator: delegator,
		Validator: op,
		Shares:    shares,
		Balance:   dec.Truncate(v.tokensFromShares(shares)),
	}
}
