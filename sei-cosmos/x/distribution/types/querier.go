package types

import (
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// querier keys
const (
	QueryParams                      = "params"
	QueryValidatorOutstandingRewards = "validator_outstanding_rewards"
	QueryValidatorCommission         = "validator_commission"
	QueryValidatorSlashes            = "validator_slashes"
	QueryDelegationRewards           = "delegation_rewards"
	QueryDelegatorTotalRewards       = "delegator_total_rewards"
	QueryDelegatorValidators         = "delegator_validators"
	QueryWithdrawAddr                = "withdraw_addr"
	QueryCommunityPool               = "community_pool"
)

// params for query 'custom/distr/validator_outstanding_rewards'
type QueryValidatorOutstandingRewardsParams struct {
	ValidatorAddress seitypes.ValAddress `json:"validator_address" yaml:"validator_address"`
}

// creates a new instance of QueryValidatorOutstandingRewardsParams
func NewQueryValidatorOutstandingRewardsParams(validatorAddr seitypes.ValAddress) QueryValidatorOutstandingRewardsParams {
	return QueryValidatorOutstandingRewardsParams{
		ValidatorAddress: validatorAddr,
	}
}

// params for query 'custom/distr/validator_commission'
type QueryValidatorCommissionParams struct {
	ValidatorAddress seitypes.ValAddress `json:"validator_address" yaml:"validator_address"`
}

// creates a new instance of QueryValidatorCommissionParams
func NewQueryValidatorCommissionParams(validatorAddr seitypes.ValAddress) QueryValidatorCommissionParams {
	return QueryValidatorCommissionParams{
		ValidatorAddress: validatorAddr,
	}
}

// params for query 'custom/distr/validator_slashes'
type QueryValidatorSlashesParams struct {
	ValidatorAddress seitypes.ValAddress `json:"validator_address" yaml:"validator_address"`
	StartingHeight   uint64              `json:"starting_height" yaml:"starting_height"`
	EndingHeight     uint64              `json:"ending_height" yaml:"ending_height"`
}

// creates a new instance of QueryValidatorSlashesParams
func NewQueryValidatorSlashesParams(validatorAddr seitypes.ValAddress, startingHeight uint64, endingHeight uint64) QueryValidatorSlashesParams {
	return QueryValidatorSlashesParams{
		ValidatorAddress: validatorAddr,
		StartingHeight:   startingHeight,
		EndingHeight:     endingHeight,
	}
}

// params for query 'custom/distr/delegation_rewards'
type QueryDelegationRewardsParams struct {
	DelegatorAddress seitypes.AccAddress `json:"delegator_address" yaml:"delegator_address"`
	ValidatorAddress seitypes.ValAddress `json:"validator_address" yaml:"validator_address"`
}

// creates a new instance of QueryDelegationRewardsParams
func NewQueryDelegationRewardsParams(delegatorAddr seitypes.AccAddress, validatorAddr seitypes.ValAddress) QueryDelegationRewardsParams {
	return QueryDelegationRewardsParams{
		DelegatorAddress: delegatorAddr,
		ValidatorAddress: validatorAddr,
	}
}

// params for query 'custom/distr/delegator_total_rewards' and 'custom/distr/delegator_validators'
type QueryDelegatorParams struct {
	DelegatorAddress seitypes.AccAddress `json:"delegator_address" yaml:"delegator_address"`
}

// creates a new instance of QueryDelegationRewardsParams
func NewQueryDelegatorParams(delegatorAddr seitypes.AccAddress) QueryDelegatorParams {
	return QueryDelegatorParams{
		DelegatorAddress: delegatorAddr,
	}
}

// params for query 'custom/distr/withdraw_addr'
type QueryDelegatorWithdrawAddrParams struct {
	DelegatorAddress seitypes.AccAddress `json:"delegator_address" yaml:"delegator_address"`
}

// NewQueryDelegatorWithdrawAddrParams creates a new instance of QueryDelegatorWithdrawAddrParams.
func NewQueryDelegatorWithdrawAddrParams(delegatorAddr seitypes.AccAddress) QueryDelegatorWithdrawAddrParams {
	return QueryDelegatorWithdrawAddrParams{DelegatorAddress: delegatorAddr}
}
