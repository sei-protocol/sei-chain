package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

// querier is used as Keeper will have duplicate methods if used directly, and gRPC names take precedence over q
type querier struct {
	Keeper
}

// NewQuerier returns an implementation of the oracle QueryServer interface
// for the provided Keeper.
func NewQuerier(keeper Keeper) types.QueryServer {
	return &querier{Keeper: keeper}
}

var _ types.QueryServer = querier{}

// Params queries params of distribution module
func (q querier) Params(c context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	var params types.Params
	q.paramSpace.GetParamSet(ctx, &params)

	return &types.QueryParamsResponse{Params: params}, nil
}

// ExchangeRate queries exchange rate of a denom
func (q querier) ExchangeRate(c context.Context, req *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if len(req.Denom) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty denom")
	}

	ctx := sdk.UnwrapSDKContext(c)
	exchangeRate, lastUpdate, err := q.GetBaseExchangeRate(ctx, req.Denom)
	if err != nil {
		return nil, err
	}

	return &types.QueryExchangeRateResponse{OracleExchangeRate: types.OracleExchangeRate{ExchangeRate: exchangeRate, LastUpdate: lastUpdate}}, nil
}

// ExchangeRates queries exchange rates of all denoms
func (q querier) ExchangeRates(c context.Context, req *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	var exchangeRates types.DenomOracleExchangeRatePairs
	q.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		if denom == utils.MicroBaseDenom {
			votePeriod := q.Keeper.GetParams(ctx).VotePeriod
			lastVotingBlockHeight := ((ctx.BlockHeight() / int64(votePeriod)) * int64(votePeriod)) - 1
			if lastVotingBlockHeight < 0 {
				lastVotingBlockHeight = 0
			}
			baseDenomRate := types.OracleExchangeRate{ExchangeRate: sdk.OneDec(), LastUpdate: sdk.NewInt(lastVotingBlockHeight)}
			exchangeRates = append(exchangeRates, types.DenomOracleExchangeRatePair{Denom: denom, OracleExchangeRate: baseDenomRate})
			return false
		}

		exchangeRates = append(exchangeRates, types.DenomOracleExchangeRatePair{Denom: denom, OracleExchangeRate: rate})
		return false
	})

	return &types.QueryExchangeRatesResponse{DenomOracleExchangeRatePairs: exchangeRates}, nil
}

// Actives queries all denoms for which exchange rates exist
func (q querier) Actives(c context.Context, req *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	denoms := []string{}
	q.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		denoms = append(denoms, denom)
		return false
	})

	return &types.QueryActivesResponse{Actives: denoms}, nil
}

// VoteTargets queries the voting target list on current vote period
func (q querier) VoteTargets(c context.Context, req *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	return &types.QueryVoteTargetsResponse{VoteTargets: q.GetVoteTargets(ctx)}, nil
}

// FeederDelegation queries the account address that the validator operator delegated oracle vote rights to
func (q querier) FeederDelegation(c context.Context, req *types.QueryFeederDelegationRequest) (*types.QueryFeederDelegationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ctx := sdk.UnwrapSDKContext(c)
	return &types.QueryFeederDelegationResponse{
		FeederAddr: q.GetFeederDelegation(ctx, valAddr).String(),
	}, nil
}

// MissCounter queries oracle miss counter of a validator
func (q querier) MissCounter(c context.Context, req *types.QueryMissCounterRequest) (*types.QueryMissCounterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ctx := sdk.UnwrapSDKContext(c)
	return &types.QueryMissCounterResponse{
		MissCounter: q.GetMissCounter(ctx, valAddr),
	}, nil
}

// AggregatePrevote queries an aggregate prevote of a validator
func (q querier) AggregatePrevote(c context.Context, req *types.QueryAggregatePrevoteRequest) (*types.QueryAggregatePrevoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ctx := sdk.UnwrapSDKContext(c)
	prevote, err := q.GetAggregateExchangeRatePrevote(ctx, valAddr)
	if err != nil {
		return nil, err
	}

	return &types.QueryAggregatePrevoteResponse{
		AggregatePrevote: prevote,
	}, nil
}

// AggregatePrevotes queries aggregate prevotes of all validators
func (q querier) AggregatePrevotes(c context.Context, req *types.QueryAggregatePrevotesRequest) (*types.QueryAggregatePrevotesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	var prevotes []types.AggregateExchangeRatePrevote
	q.IterateAggregateExchangeRatePrevotes(ctx, func(_ sdk.ValAddress, prevote types.AggregateExchangeRatePrevote) bool {
		prevotes = append(prevotes, prevote)
		return false
	})

	return &types.QueryAggregatePrevotesResponse{
		AggregatePrevotes: prevotes,
	}, nil
}

// AggregateVote queries an aggregate vote of a validator
func (q querier) AggregateVote(c context.Context, req *types.QueryAggregateVoteRequest) (*types.QueryAggregateVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ctx := sdk.UnwrapSDKContext(c)
	vote, err := q.GetAggregateExchangeRateVote(ctx, valAddr)
	if err != nil {
		return nil, err
	}

	return &types.QueryAggregateVoteResponse{
		AggregateVote: vote,
	}, nil
}

// AggregateVotes queries aggregate votes of all validators
func (q querier) AggregateVotes(c context.Context, req *types.QueryAggregateVotesRequest) (*types.QueryAggregateVotesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	var votes []types.AggregateExchangeRateVote
	q.IterateAggregateExchangeRateVotes(ctx, func(_ sdk.ValAddress, vote types.AggregateExchangeRateVote) bool {
		votes = append(votes, vote)
		return false
	})

	return &types.QueryAggregateVotesResponse{
		AggregateVotes: votes,
	}, nil
}
