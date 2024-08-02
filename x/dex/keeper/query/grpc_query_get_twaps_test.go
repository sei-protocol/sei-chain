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

const GENESIS_TIME uint64 = 3600

func TestGetTwapsNoPriceSnapshot(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, keepertest.TestContract, keepertest.TestPair)
	wctx := sdk.WrapSDKContext(ctx)
	var lookback uint64 = 10
	request := types.QueryGetTwapsRequest{
		ContractAddr:    keepertest.TestContract,
		LookbackSeconds: lookback,
	}
	expectedResponse := types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &keepertest.TestPair,
				Twap:            sdk.ZeroDec(),
				LookbackSeconds: lookback,
			},
		},
	}
	wrapper := query.KeeperWrapper{Keeper: keeper}
	t.Run("No snapshot", func(t *testing.T) {
		response, err := wrapper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}

func TestGetTwapsOnePriceSnapshot(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, keepertest.TestContract, keepertest.TestPair)
	ctx = ctx.WithBlockTime(time.Unix(int64(GENESIS_TIME)+5, 0))
	wctx := sdk.WrapSDKContext(ctx)

	snapshotPrice := sdk.MustNewDecFromStr("100.00")
	keeper.SetPriceState(ctx, types.Price{
		SnapshotTimestampInSeconds: GENESIS_TIME,
		Price:                      snapshotPrice,
		Pair:                       &keepertest.TestPair,
	}, keepertest.TestContract)

	var lookback uint64 = 10
	request := types.QueryGetTwapsRequest{
		ContractAddr:    keepertest.TestContract,
		LookbackSeconds: lookback,
	}
	expectedResponse := types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &keepertest.TestPair,
				Twap:            snapshotPrice,
				LookbackSeconds: lookback,
			},
		},
	}
	wrapper := query.KeeperWrapper{Keeper: keeper}
	t.Run("One snapshot", func(t *testing.T) {
		response, err := wrapper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})

	lookback = 4
	request = types.QueryGetTwapsRequest{
		ContractAddr:    keepertest.TestContract,
		LookbackSeconds: lookback,
	}
	expectedResponse = types.QueryGetTwapsResponse{
		Twaps: []*types.Twap{
			{
				Pair:            &keepertest.TestPair,
				Twap:            snapshotPrice,
				LookbackSeconds: lookback,
			},
		},
	}
	t.Run("One old snapshot", func(t *testing.T) {
		response, err := wrapper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}

func TestGetTwapsMultipleSnapshots(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, keepertest.TestContract, keepertest.TestPair)
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
			Pair:                       &keepertest.TestPair,
		}, keepertest.TestContract)
	}

	var lookback uint64 = 20
	request := types.QueryGetTwapsRequest{
		ContractAddr:    keepertest.TestContract,
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
				Pair:            &keepertest.TestPair,
				Twap:            expectedTwap,
				LookbackSeconds: lookback,
			},
		},
	}
	wrapper := query.KeeperWrapper{Keeper: keeper}
	t.Run("Multiple snapshots", func(t *testing.T) {
		response, err := wrapper.GetTwaps(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}
