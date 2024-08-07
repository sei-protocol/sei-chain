package query_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestEachPeriodOneDataPoint(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 1)
	keepertest.SeedPriceSnapshot(ctx, keeper, "101", 2)
	keepertest.SeedPriceSnapshot(ctx, keeper, "99", 3) // should not be included since end is exclusive

	ctx = ctx.WithBlockTime(time.Unix(3, 0))
	wctx := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	resp, err := wrapper.GetHistoricalPrices(wctx, &types.QueryGetHistoricalPricesRequest{
		ContractAddr:          keepertest.TestContract,
		PriceDenom:            keepertest.TestPair.PriceDenom,
		AssetDenom:            keepertest.TestPair.AssetDenom,
		PeriodLengthInSeconds: 1,
		NumOfPeriods:          2,
	})
	require.Nil(t, err)
	require.Equal(t, 2, len(resp.Prices))
	require.Equal(t, uint64(2), resp.Prices[0].BeginTimestamp)
	require.Equal(t, uint64(3), resp.Prices[0].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.Prices[0].Open)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.Prices[0].High)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.Prices[0].Low)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.Prices[0].Close)
	require.Equal(t, uint64(1), resp.Prices[1].BeginTimestamp)
	require.Equal(t, uint64(2), resp.Prices[1].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Open)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].High)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Low)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Close)
}

func TestEachPeriodMultipleDataPoints(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 1)
	keepertest.SeedPriceSnapshot(ctx, keeper, "101", 2)
	keepertest.SeedPriceSnapshot(ctx, keeper, "102", 3)
	keepertest.SeedPriceSnapshot(ctx, keeper, "99", 4)
	keepertest.SeedPriceSnapshot(ctx, keeper, "98", 5) // should not be included since end is exclusive

	ctx = ctx.WithBlockTime(time.Unix(5, 0))
	wctx := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	resp, err := wrapper.GetHistoricalPrices(wctx, &types.QueryGetHistoricalPricesRequest{
		ContractAddr:          keepertest.TestContract,
		PriceDenom:            keepertest.TestPair.PriceDenom,
		AssetDenom:            keepertest.TestPair.AssetDenom,
		PeriodLengthInSeconds: 2,
		NumOfPeriods:          2,
	})
	require.Nil(t, err)
	require.Equal(t, 2, len(resp.Prices))
	require.Equal(t, uint64(3), resp.Prices[0].BeginTimestamp)
	require.Equal(t, uint64(5), resp.Prices[0].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Open)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].High)
	require.Equal(t, sdk.MustNewDecFromStr("99"), *resp.Prices[0].Low)
	require.Equal(t, sdk.MustNewDecFromStr("99"), *resp.Prices[0].Close)
	require.Equal(t, uint64(1), resp.Prices[1].BeginTimestamp)
	require.Equal(t, uint64(3), resp.Prices[1].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Open)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.Prices[1].High)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Low)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.Prices[1].Close)
}

func TestMissingDataPoints(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 1)
	keepertest.SeedPriceSnapshot(ctx, keeper, "102", 3)
	keepertest.SeedPriceSnapshot(ctx, keeper, "98", 5) // should not be included since end is exclusive

	ctx = ctx.WithBlockTime(time.Unix(5, 0))
	wctx := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	resp, err := wrapper.GetHistoricalPrices(wctx, &types.QueryGetHistoricalPricesRequest{
		ContractAddr:          keepertest.TestContract,
		PriceDenom:            keepertest.TestPair.PriceDenom,
		AssetDenom:            keepertest.TestPair.AssetDenom,
		PeriodLengthInSeconds: 1,
		NumOfPeriods:          4,
	})
	require.Nil(t, err)
	require.Equal(t, 4, len(resp.Prices))
	require.Equal(t, uint64(4), resp.Prices[0].BeginTimestamp)
	require.Equal(t, uint64(5), resp.Prices[0].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Open)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].High)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Low)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Close)
	require.Equal(t, uint64(3), resp.Prices[1].BeginTimestamp)
	require.Equal(t, uint64(4), resp.Prices[1].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[1].Open)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[1].High)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[1].Low)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[1].Close)
	require.Equal(t, uint64(2), resp.Prices[2].BeginTimestamp)
	require.Equal(t, uint64(3), resp.Prices[2].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[2].Open)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[2].High)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[2].Low)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[2].Close)
	require.Equal(t, uint64(1), resp.Prices[3].BeginTimestamp)
	require.Equal(t, uint64(2), resp.Prices[3].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[3].Open)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[3].High)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[3].Low)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[3].Close)
}

func TestDataPointsNotEarlyEnoughFullBar(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 2)
	keepertest.SeedPriceSnapshot(ctx, keeper, "102", 3) // should not be included since end is exclusive

	ctx = ctx.WithBlockTime(time.Unix(3, 0))
	wctx := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	resp, err := wrapper.GetHistoricalPrices(wctx, &types.QueryGetHistoricalPricesRequest{
		ContractAddr:          keepertest.TestContract,
		PriceDenom:            keepertest.TestPair.PriceDenom,
		AssetDenom:            keepertest.TestPair.AssetDenom,
		PeriodLengthInSeconds: 1,
		NumOfPeriods:          2,
	})
	require.Nil(t, err)
	require.Equal(t, 2, len(resp.Prices))
	require.Equal(t, uint64(2), resp.Prices[0].BeginTimestamp)
	require.Equal(t, uint64(3), resp.Prices[0].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[0].Open)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[0].High)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[0].Low)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[0].Close)
	require.Equal(t, uint64(1), resp.Prices[1].BeginTimestamp)
	require.Equal(t, uint64(2), resp.Prices[1].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("0"), *resp.Prices[1].Open)
	require.Equal(t, sdk.MustNewDecFromStr("0"), *resp.Prices[1].High)
	require.Equal(t, sdk.MustNewDecFromStr("0"), *resp.Prices[1].Low)
	require.Equal(t, sdk.MustNewDecFromStr("0"), *resp.Prices[1].Close)
}

func TestDataPointsNotEarlyEnoughPartialBar(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 2)
	keepertest.SeedPriceSnapshot(ctx, keeper, "102", 3)
	keepertest.SeedPriceSnapshot(ctx, keeper, "98", 5) // should not be included since end is exclusive

	ctx = ctx.WithBlockTime(time.Unix(5, 0))
	wctx := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	resp, err := wrapper.GetHistoricalPrices(wctx, &types.QueryGetHistoricalPricesRequest{
		ContractAddr:          keepertest.TestContract,
		PriceDenom:            keepertest.TestPair.PriceDenom,
		AssetDenom:            keepertest.TestPair.AssetDenom,
		PeriodLengthInSeconds: 2,
		NumOfPeriods:          2,
	})
	require.Nil(t, err)
	require.Equal(t, 2, len(resp.Prices))
	require.Equal(t, uint64(3), resp.Prices[0].BeginTimestamp)
	require.Equal(t, uint64(5), resp.Prices[0].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Open)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].High)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Low)
	require.Equal(t, sdk.MustNewDecFromStr("102"), *resp.Prices[0].Close)
	require.Equal(t, uint64(1), resp.Prices[1].BeginTimestamp)
	require.Equal(t, uint64(3), resp.Prices[1].EndTimestamp)
	require.Equal(t, sdk.MustNewDecFromStr("0"), *resp.Prices[1].Open)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].High)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Low)
	require.Equal(t, sdk.MustNewDecFromStr("100"), *resp.Prices[1].Close)
}
