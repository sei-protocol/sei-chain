package keeper

import (
	"bytes"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

func TestQueryParams(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)

	querier := NewQuerier(input.OracleKeeper)
	res, err := querier.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)

	require.Equal(t, input.OracleKeeper.GetParams(input.Ctx), res.Params)
}

func TestQueryExchangeRate(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	rate := sdk.NewDec(1700)
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom, rate)

	// Query to grpc
	res, err := querier.ExchangeRate(ctx, &types.QueryExchangeRateRequest{
		Denom: utils.MicroAtomDenom,
	})
	require.NoError(t, err)
	require.Equal(t, rate, res.OracleExchangeRate.ExchangeRate)
}

func TestQueryEmptyExchangeRates(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	res, err := querier.ExchangeRates(ctx, &types.QueryExchangeRatesRequest{})
	require.NoError(t, err)

	require.Equal(t, types.DenomOracleExchangeRatePairs{}, res.DenomOracleExchangeRatePairs)
}

func TestQueryExchangeRates(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	rate := sdk.NewDec(1700)
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom, rate)
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroSeiDenom, rate)

	res, err := querier.ExchangeRates(ctx, &types.QueryExchangeRatesRequest{})
	require.NoError(t, err)

	require.Equal(t, types.DenomOracleExchangeRatePairs{
		types.NewDenomOracleExchangeRatePair(utils.MicroAtomDenom, rate, sdk.ZeroInt()),
		types.NewDenomOracleExchangeRatePair(utils.MicroSeiDenom, rate, sdk.ZeroInt()),
	}, res.DenomOracleExchangeRatePairs)
}

func TestQueryFeederDelegation(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	input.OracleKeeper.SetFeederDelegation(input.Ctx, ValAddrs[0], Addrs[1])

	res, err := querier.FeederDelegation(ctx, &types.QueryFeederDelegationRequest{
		ValidatorAddr: ValAddrs[0].String(),
	})
	require.NoError(t, err)

	require.Equal(t, Addrs[1].String(), res.FeederAddr)
}

func TestQueryAggregateVote(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	vote1 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "", ExchangeRate: sdk.OneDec()}}, ValAddrs[0])
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[0], vote1)
	vote2 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "", ExchangeRate: sdk.OneDec()}}, ValAddrs[1])
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[1], vote2)

	// validator 0 address params
	res, err := querier.AggregateVote(ctx, &types.QueryAggregateVoteRequest{
		ValidatorAddr: ValAddrs[0].String(),
	})
	require.NoError(t, err)
	require.Equal(t, vote1, res.AggregateVote)

	// validator 1 address params
	res, err = querier.AggregateVote(ctx, &types.QueryAggregateVoteRequest{
		ValidatorAddr: ValAddrs[1].String(),
	})
	require.NoError(t, err)
	require.Equal(t, vote2, res.AggregateVote)
}

func TestQueryAggregateVotes(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	vote1 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "", ExchangeRate: sdk.OneDec()}}, ValAddrs[0])
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[0], vote1)
	vote2 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "", ExchangeRate: sdk.OneDec()}}, ValAddrs[1])
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[1], vote2)
	vote3 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "", ExchangeRate: sdk.OneDec()}}, ValAddrs[2])
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[2], vote3)

	expectedVotes := []types.AggregateExchangeRateVote{vote1, vote2, vote3}
	sort.SliceStable(expectedVotes, func(i, j int) bool {
		addr1, _ := sdk.ValAddressFromBech32(expectedVotes[i].Voter)
		addr2, _ := sdk.ValAddressFromBech32(expectedVotes[j].Voter)
		return bytes.Compare(addr1, addr2) == -1
	})

	res, err := querier.AggregateVotes(ctx, &types.QueryAggregateVotesRequest{})
	require.NoError(t, err)
	require.Equal(t, expectedVotes, res.AggregateVotes)
}

func TestQueryVoteTargets(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)

	voteTargets := []string{"denom", "denom2", "denom3"}
	for _, target := range voteTargets {
		input.OracleKeeper.SetVoteTarget(input.Ctx, target)
	}

	res, err := querier.VoteTargets(ctx, &types.QueryVoteTargetsRequest{})
	require.NoError(t, err)
	require.Equal(t, voteTargets, res.VoteTargets)
}

func TestQueryPriceSnapshotHistory(t *testing.T) {
	input := CreateTestInput(t)
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)

	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(11),
				LastUpdate:   sdk.NewInt(20),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(12),
				LastUpdate:   sdk.NewInt(20),
			}),
		}, 1),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(21),
				LastUpdate:   sdk.NewInt(30),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(22),
				LastUpdate:   sdk.NewInt(30),
			}),
		}, 2),
	}

	input.OracleKeeper.SetPriceSnapshot(input.Ctx, priceSnapshots[0])
	input.OracleKeeper.SetPriceSnapshot(input.Ctx, priceSnapshots[1])

	res, err := querier.PriceSnapshotHistory(ctx, &types.QueryPriceSnapshotHistoryRequest{})
	require.NoError(t, err)

	require.Equal(t, priceSnapshots, res.PriceSnapshots)
}

func TestQueryTwaps(t *testing.T) {
	input := CreateTestInput(t)
	input.Ctx = input.Ctx.WithBlockTime(time.Unix(5400, 0))
	ctx := sdk.WrapSDKContext(input.Ctx)
	querier := NewQuerier(input.OracleKeeper)
	_, err := querier.Twaps(ctx, &types.QueryTwapsRequest{LookbackSeconds: 3600})
	require.Error(t, err)

	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(40),
				LastUpdate:   sdk.NewInt(1800),
			}),
		}, 1200),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(10),
				LastUpdate:   sdk.NewInt(3600),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(3600),
			}),
		}, 3600),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(4500),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(40),
				LastUpdate:   sdk.NewInt(4500),
			}),
		}, 4500),
	}
	for _, snap := range priceSnapshots {
		input.OracleKeeper.SetPriceSnapshot(input.Ctx, snap)
	}
	twaps, err := querier.Twaps(ctx, &types.QueryTwapsRequest{LookbackSeconds: 3600})
	require.NoError(t, err)
	oracleTwaps := twaps.OracleTwaps
	require.Equal(t, 2, len(oracleTwaps))
	atomTwap := oracleTwaps[0]
	ethTwap := oracleTwaps[1]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(3600), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(35), atomTwap.Twap)

	require.Equal(t, utils.MicroEthDenom, ethTwap.Denom)
	require.Equal(t, int64(1800), ethTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(15), ethTwap.Twap)
}
