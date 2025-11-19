package types

import (
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// query endpoints supported by the staking Querier
const (
	QueryValidators                    = "validators"
	QueryValidator                     = "validator"
	QueryDelegatorDelegations          = "delegatorDelegations"
	QueryDelegatorUnbondingDelegations = "delegatorUnbondingDelegations"
	QueryRedelegations                 = "redelegations"
	QueryValidatorDelegations          = "validatorDelegations"
	QueryValidatorRedelegations        = "validatorRedelegations"
	QueryValidatorUnbondingDelegations = "validatorUnbondingDelegations"
	QueryDelegation                    = "delegation"
	QueryUnbondingDelegation           = "unbondingDelegation"
	QueryDelegatorValidators           = "delegatorValidators"
	QueryDelegatorValidator            = "delegatorValidator"
	QueryPool                          = "pool"
	QueryParameters                    = "parameters"
	QueryHistoricalInfo                = "historicalInfo"
)

// defines the params for the following queries:
// - 'custom/staking/delegatorDelegations'
// - 'custom/staking/delegatorUnbondingDelegations'
// - 'custom/staking/delegatorValidators'
type QueryDelegatorParams struct {
	DelegatorAddr seitypes.AccAddress
}

func NewQueryDelegatorParams(delegatorAddr seitypes.AccAddress) QueryDelegatorParams {
	return QueryDelegatorParams{
		DelegatorAddr: delegatorAddr,
	}
}

// defines the params for the following queries:
// - 'custom/staking/validator'
// - 'custom/staking/validatorDelegations'
// - 'custom/staking/validatorUnbondingDelegations'
type QueryValidatorParams struct {
	ValidatorAddr seitypes.ValAddress
	Page, Limit   int
}

func NewQueryValidatorParams(validatorAddr seitypes.ValAddress, page, limit int) QueryValidatorParams {
	return QueryValidatorParams{
		ValidatorAddr: validatorAddr,
		Page:          page,
		Limit:         limit,
	}
}

// defines the params for the following queries:
// - 'custom/staking/redelegation'
type QueryRedelegationParams struct {
	DelegatorAddr    seitypes.AccAddress
	SrcValidatorAddr seitypes.ValAddress
	DstValidatorAddr seitypes.ValAddress
}

func NewQueryRedelegationParams(delegatorAddr seitypes.AccAddress, srcValidatorAddr, dstValidatorAddr seitypes.ValAddress) QueryRedelegationParams {
	return QueryRedelegationParams{
		DelegatorAddr:    delegatorAddr,
		SrcValidatorAddr: srcValidatorAddr,
		DstValidatorAddr: dstValidatorAddr,
	}
}

// QueryValidatorsParams defines the params for the following queries:
// - 'custom/staking/validators'
type QueryValidatorsParams struct {
	Page, Limit int
	Status      string
}

func NewQueryValidatorsParams(page, limit int, status string) QueryValidatorsParams {
	return QueryValidatorsParams{page, limit, status}
}
