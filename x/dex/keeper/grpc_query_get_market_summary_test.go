package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetMarketSummary(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	seedPriceSnapshot(ctx, keeper, "100", 1)
	seedPriceSnapshot(ctx, keeper, "101", 2)
	seedPriceSnapshot(ctx, keeper, "99", 3)

	ctx = ctx.WithBlockTime(time.Unix(4, 0))
	wctx := sdk.WrapSDKContext(ctx)
	resp, err := keeper.GetMarketSummary(wctx, &types.QueryGetMarketSummaryRequest{
		ContractAddr:      TEST_CONTRACT,
		PriceDenom:        TEST_PAIR.PriceDenom,
		AssetDenom:        TEST_PAIR.AssetDenom,
		LookbackInSeconds: 4,
	})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("99"), *resp.LowPrice)
	require.Equal(t, sdk.MustNewDecFromStr("99"), *resp.LastPrice)
	require.Equal(t, sdk.MustNewDecFromStr("101"), *resp.HighPrice)
}
