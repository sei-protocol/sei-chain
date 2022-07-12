package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const GENESIS_TIME uint64 = 3600

var (
	TEST_TICKSIZE = sdk.OneDec()
	TEST_PAIR     = types.Pair{
		PriceDenom: TEST_PRICE_DENOM,
		AssetDenom: TEST_ASSET_DENOM,
		Ticksize:   &TEST_TICKSIZE,
	}
)

func TestGetTwapsNoPriceSnapshot(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, TEST_CONTRACT, TEST_PAIR)
	wctx := sdk.WrapSDKContext(ctx)
	var lookback uint64 = 10
	request := types.QueryGetTwapsRequest{
		ContractAddr:    TEST_CONTRACT,
		LookbackSeconds: lookback,
	}
	expectedResponse := types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &TEST_PAIR,
				Twap:            sdk.ZeroDec(),
				LookbackSeconds: lookback,
			},
		},
	}
	t.Run("No snapshot", func(t *testing.T) {
		response, err := keeper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}

func TestGetTwapsOnePriceSnapshot(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, TEST_CONTRACT, TEST_PAIR)
	ctx = ctx.WithBlockTime(time.Unix(int64(GENESIS_TIME)+5, 0))
	wctx := sdk.WrapSDKContext(ctx)

	snapshotPrice := sdk.MustNewDecFromStr("100.00")
	keeper.SetPriceState(ctx, types.Price{
		SnapshotTimestampInSeconds: GENESIS_TIME,
		Price:                      snapshotPrice,
		Pair:                       &TEST_PAIR,
	}, TEST_CONTRACT, 0)

	var lookback uint64 = 10
	request := types.QueryGetTwapsRequest{
		ContractAddr:    TEST_CONTRACT,
		LookbackSeconds: lookback,
	}
	expectedResponse := types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &TEST_PAIR,
				Twap:            snapshotPrice,
				LookbackSeconds: lookback,
			},
		},
	}
	t.Run("One snapshot", func(t *testing.T) {
		response, err := keeper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})

	lookback = 4
	request = types.QueryGetTwapsRequest{
		ContractAddr:    TEST_CONTRACT,
		LookbackSeconds: lookback,
	}
	expectedResponse = types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &TEST_PAIR,
				Twap:            snapshotPrice,
				LookbackSeconds: lookback,
			},
		},
	}
	t.Run("One old snapshot", func(t *testing.T) {
		response, err := keeper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}

func TestGetTwapsMultipleSnapshots(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, TEST_CONTRACT, TEST_PAIR)
	ctx = ctx.WithBlockTime(time.Unix(int64(GENESIS_TIME)+20, 0))
	wctx := sdk.WrapSDKContext(ctx)

	snapshotPrices := []sdk.Dec{
		sdk.MustNewDecFromStr("100.00"),
		sdk.MustNewDecFromStr("98.50"),
		sdk.MustNewDecFromStr("101.00"),
	}
	timestampDeltas := []uint64{0, 10, 15}
	for i := range snapshotPrices {
		keeper.SetPriceState(ctx, types.Price{
			SnapshotTimestampInSeconds: GENESIS_TIME + timestampDeltas[i],
			Price:                      snapshotPrices[i],
			Pair:                       &TEST_PAIR,
		}, TEST_CONTRACT, uint64(i))
	}

	var lookback uint64 = 20
	request := types.QueryGetTwapsRequest{
		ContractAddr:    TEST_CONTRACT,
		LookbackSeconds: lookback,
	}
	expectedTwap := snapshotPrices[0].MulInt64(10).Add(
		snapshotPrices[1].MulInt64(15 - 10),
	).Add(
		snapshotPrices[2].MulInt64(20 - 15),
	).QuoInt64(20)
	require.Equal(t, sdk.MustNewDecFromStr("99.875"), expectedTwap)
	expectedResponse := types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &TEST_PAIR,
				Twap:            expectedTwap,
				LookbackSeconds: lookback,
			},
		},
	}
	t.Run("Multiple snapshots", func(t *testing.T) {
		response, err := keeper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}
