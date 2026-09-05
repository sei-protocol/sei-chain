package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

func TestDeprecatedQueries(t *testing.T) {
	querier := keeper.NewQuerier(keeper.Keeper{})
	ctx := context.Background()

	testCases := []struct {
		name  string
		query func(*testing.T) error
	}{
		{
			name: "params",
			query: func(t *testing.T) error {
				response, err := querier.Params(ctx, &types.QueryParamsRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "exchange rate",
			query: func(t *testing.T) error {
				response, err := querier.ExchangeRate(ctx, &types.QueryExchangeRateRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "exchange rates",
			query: func(t *testing.T) error {
				response, err := querier.ExchangeRates(ctx, &types.QueryExchangeRatesRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "actives",
			query: func(t *testing.T) error {
				response, err := querier.Actives(ctx, &types.QueryActivesRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "vote targets",
			query: func(t *testing.T) error {
				response, err := querier.VoteTargets(ctx, &types.QueryVoteTargetsRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "price snapshot history",
			query: func(t *testing.T) error {
				response, err := querier.PriceSnapshotHistory(ctx, &types.QueryPriceSnapshotHistoryRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "twaps",
			query: func(t *testing.T) error {
				response, err := querier.Twaps(ctx, &types.QueryTwapsRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "feeder delegation",
			query: func(t *testing.T) error {
				response, err := querier.FeederDelegation(ctx, &types.QueryFeederDelegationRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "vote penalty counter",
			query: func(t *testing.T) error {
				response, err := querier.VotePenaltyCounter(ctx, &types.QueryVotePenaltyCounterRequest{})
				require.Nil(t, response)
				return err
			},
		},
		{
			name: "slash window",
			query: func(t *testing.T) error {
				response, err := querier.SlashWindow(ctx, &types.QuerySlashWindowRequest{})
				require.Nil(t, response)
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.query(t), types.ErrOracleDeprecated)
		})
	}
}
