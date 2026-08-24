package keeper

import (
	"context"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

type querier struct{}

// NewQuerier returns an implementation of the oracle QueryServer interface
// that rejects queries because the module is deprecated.
func NewQuerier(_ Keeper) types.QueryServer {
	return &querier{}
}

var _ types.QueryServer = querier{}

func (querier) Params(context.Context, *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) ExchangeRate(context.Context, *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) ExchangeRates(context.Context, *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) Actives(context.Context, *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) VoteTargets(context.Context, *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) PriceSnapshotHistory(context.Context, *types.QueryPriceSnapshotHistoryRequest) (*types.QueryPriceSnapshotHistoryResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) Twaps(context.Context, *types.QueryTwapsRequest) (*types.QueryTwapsResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) FeederDelegation(context.Context, *types.QueryFeederDelegationRequest) (*types.QueryFeederDelegationResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) VotePenaltyCounter(context.Context, *types.QueryVotePenaltyCounterRequest) (*types.QueryVotePenaltyCounterResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (querier) SlashWindow(context.Context, *types.QuerySlashWindowRequest) (*types.QuerySlashWindowResponse, error) {
	return nil, types.ErrOracleDeprecated
}
