package types

import (
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// Defines the prefix of each query path
const (
	QueryParameters           = "parameters"
	QueryExchangeRate         = "exchangeRate"
	QueryExchangeRates        = "exchangeRates"
	QueryPriceSnapshotHistory = "priceSnapshotHistory"
	QueryTwaps                = "twaps"
	QueryActives              = "actives"
	QueryFeederDelegation     = "feederDelegation"
	QueryVotePenaltyCounter   = "votePenaltyCounter"
	QueryAggregateVote        = "aggregateVote"
	QueryAggregateVotes       = "aggregateVotes"
	QueryVoteTargets          = "voteTargets"
)

// QueryExchangeRateParams defines the params for the following queries:
// - 'custom/oracle/exchange_rate'
type QueryExchangeRateParams struct {
	Denom string
}

// NewQueryExchangeRateParams returns params for exchange_rate query
func NewQueryExchangeRateParams(denom string) QueryExchangeRateParams {
	return QueryExchangeRateParams{denom}
}

// QueryTwapParams defines the params for the following queries:
// - 'custom/oracle/twap'
type QueryTwapsParams struct {
	LookbackSeconds int64
}

// NewQueryExchangeRateParams returns params for exchange_rate query
func NewQueryTwapsParams(lookbackSeconds int64) QueryTwapsParams {
	return QueryTwapsParams{lookbackSeconds}
}

// QueryVotesParams defines the params for the following queries:
// - 'custom/oracle/votes'
type QueryVotesParams struct {
	Voter seitypes.ValAddress
	Denom string
}

// NewQueryVotesParams returns params for exchange_rate votes query
func NewQueryVotesParams(voter seitypes.ValAddress, denom string) QueryVotesParams {
	return QueryVotesParams{voter, denom}
}

// QueryFeederDelegationParams defeins the params for the following queries:
// - 'custom/oracle/feederDelegation'
type QueryFeederDelegationParams struct {
	Validator seitypes.ValAddress
}

// NewQueryFeederDelegationParams returns params for feeder delegation query
func NewQueryFeederDelegationParams(validator seitypes.ValAddress) QueryFeederDelegationParams {
	return QueryFeederDelegationParams{validator}
}

// QueryMissCounterParams defines the params for the following queries:
// - 'custom/oracle/missCounter'
type QueryVotePenaltyCounterParams struct {
	Validator seitypes.ValAddress
}

// NewQueryVotePenaltyCounterParams returns params for feeder delegation query
func NewQueryVotePenaltyCounterParams(validator seitypes.ValAddress) QueryVotePenaltyCounterParams {
	return QueryVotePenaltyCounterParams{validator}
}

// QueryAggregateVoteParams defines the params for the following queries:
// - 'custom/oracle/aggregateVote'
type QueryAggregateVoteParams struct {
	Validator seitypes.ValAddress
}

// NewQueryAggregateVoteParams returns params for feeder delegation query
func NewQueryAggregateVoteParams(validator seitypes.ValAddress) QueryAggregateVoteParams {
	return QueryAggregateVoteParams{validator}
}
