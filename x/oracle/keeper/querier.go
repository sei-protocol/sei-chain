package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
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

func (q querier) Deprecated_Params(c context.Context, params *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return q.Params(c, params)
}

// Params queries params of distribution module
func (q querier) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	var params types.Params
	q.paramSpace.GetParamSet(ctx, &params)

	return &types.QueryParamsResponse{Params: params}, nil
}

func (q querier) Deprecated_ExchangeRate(c context.Context, req *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	return q.ExchangeRate(c, req)
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
	exchangeRate, lastUpdate, lastUpdateTimestamp, err := q.GetBaseExchangeRate(ctx, req.Denom)
	if err != nil {
		return nil, err
	}

	return &types.QueryExchangeRateResponse{OracleExchangeRate: types.OracleExchangeRate{
		ExchangeRate: exchangeRate, LastUpdate: lastUpdate, LastUpdateTimestamp: lastUpdateTimestamp,
	}}, nil
}

func (q querier) Deprecated_ExchangeRates(c context.Context, _ *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	return q.ExchangeRates(c, nil)
}

// ExchangeRates queries exchange rates of all denoms
func (q querier) ExchangeRates(c context.Context, _ *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	exchangeRates := []types.DenomOracleExchangeRatePair{}
	q.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		exchangeRates = append(exchangeRates, types.DenomOracleExchangeRatePair{Denom: denom, OracleExchangeRate: rate})
		return false
	})

	return &types.QueryExchangeRatesResponse{DenomOracleExchangeRatePairs: exchangeRates}, nil
}

func (q querier) Deprecated_Actives(c context.Context, req *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	return q.Actives(c, req)
}

// Actives queries all denoms for which exchange rates exist
func (q querier) Actives(c context.Context, _ *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	denoms := []string{}
	q.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		denoms = append(denoms, denom)
		return false
	})

	return &types.QueryActivesResponse{Actives: denoms}, nil
}

func (q querier) Deprecated_VoteTargets(c context.Context, req *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	return q.VoteTargets(c, req)
}

// VoteTargets queries the voting target list on current vote period
func (q querier) VoteTargets(c context.Context, _ *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	return &types.QueryVoteTargetsResponse{VoteTargets: q.GetVoteTargets(ctx)}, nil
}

func (q querier) Deprecated_PriceSnapshotHistory(c context.Context, _ *types.QueryPriceSnapshotHistoryRequest) (*types.QueryPriceSnapshotHistoryResponse, error) {
	return q.PriceSnapshotHistory(c, nil)
}
func (q querier) PriceSnapshotHistory(c context.Context, _ *types.QueryPriceSnapshotHistoryRequest) (*types.QueryPriceSnapshotHistoryResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	priceSnapshots := types.PriceSnapshots{}
	q.IteratePriceSnapshots(ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		priceSnapshots = append(priceSnapshots, snapshot)
		return false
	})
	response := types.QueryPriceSnapshotHistoryResponse{PriceSnapshots: priceSnapshots}
	return &response, nil
}

func (q querier) Deprecated_Twaps(c context.Context, req *types.QueryTwapsRequest) (*types.QueryTwapsResponse, error) {
	return q.Twaps(c, req)
}
func (q querier) Twaps(c context.Context, req *types.QueryTwapsRequest) (*types.QueryTwapsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	twaps, err := q.CalculateTwaps(ctx, req.LookbackSeconds)
	if err != nil {
		return nil, err
	}
	response := types.QueryTwapsResponse{OracleTwaps: twaps}
	return &response, nil
}

func (q querier) Deprecated_FeederDelegation(c context.Context, req *types.QueryFeederDelegationRequest) (*types.QueryFeederDelegationResponse, error) {
	return q.FeederDelegation(c, req)
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

func (q querier) Deprecated_VotePenaltyCounter(c context.Context, req *types.QueryVotePenaltyCounterRequest) (*types.QueryVotePenaltyCounterResponse, error) {
	return q.VotePenaltyCounter(c, req)
}

// MissCounter queries oracle miss counter of a validator
func (q querier) VotePenaltyCounter(c context.Context, req *types.QueryVotePenaltyCounterRequest) (*types.QueryVotePenaltyCounterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ctx := sdk.UnwrapSDKContext(c)
	return &types.QueryVotePenaltyCounterResponse{
		VotePenaltyCounter: &types.VotePenaltyCounter{
			MissCount:    q.GetMissCount(ctx, valAddr),
			AbstainCount: q.GetAbstainCount(ctx, valAddr),
			SuccessCount: q.GetSuccessCount(ctx, valAddr),
		},
	}, nil
}

func (q querier) Deprecated_SlashWindow(goCtx context.Context, _ *types.QuerySlashWindowRequest) (*types.QuerySlashWindowResponse, error) {
	return q.SlashWindow(goCtx, nil)
}
func (q querier) SlashWindow(goCtx context.Context, _ *types.QuerySlashWindowRequest) (*types.QuerySlashWindowResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := q.GetParams(ctx)
	// The window progress is the number of vote periods that have been completed in the current slashing window. With a vote period of 1, this will be equivalent to the number of blocks that have progressed in the slash window.
	return &types.QuerySlashWindowResponse{
		WindowProgress: (uint64(ctx.BlockHeight()) % params.SlashWindow) /
			params.VotePeriod,
	}, nil
}
