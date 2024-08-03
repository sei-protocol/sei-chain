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

func TestGetPrice(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 1)
	keepertest.SeedPriceSnapshot(ctx, keeper, "101", 2)
	keepertest.SeedPriceSnapshot(ctx, keeper, "99", 3)

	ctx = ctx.WithBlockTime(time.Unix(4, 0))
	wctx := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	resp, err := wrapper.GetPrice(wctx, &types.QueryGetPriceRequest{
		ContractAddr: keepertest.TestContract,
		PriceDenom:   keepertest.TestPair.PriceDenom,
		AssetDenom:   keepertest.TestPair.AssetDenom,
		Timestamp:    2,
	})
	require.Nil(t, err)
	require.Equal(t, true, resp.Found)
	require.Equal(t, sdk.MustNewDecFromStr("101"), resp.Price.Price)
}
