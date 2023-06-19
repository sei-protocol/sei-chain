package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/stretchr/testify/require"
)

func TestCancelOrder(t *testing.T) {
	// store a long limit order to the orderbook
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetLongBook(ctx, keepertest.TestContract, types.LongBook{
		Price: sdk.OneDec(),
		Entry: &types.OrderEntry{
			Price:      sdk.OneDec(),
			Quantity:   sdk.MustNewDecFromStr("2"),
			PriceDenom: keepertest.TestPriceDenom,
			AssetDenom: keepertest.TestAssetDenom,
			Allocations: []*types.Allocation{
				{
					Account:  keepertest.TestAccount,
					OrderId:  1,
					Quantity: sdk.MustNewDecFromStr("2"),
				},
			},
		},
	})

	// cancel order
	msg := &types.MsgCancelOrders{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec(),
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
	}
	keeper.AddRegisteredPair(ctx, keepertest.TestContract, keepertest.TestPair)
	keeper.SetPriceTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.PriceTicksize)
	keeper.SetQuantityTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.QuantityTicksize)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	_, err := server.CancelOrders(wctx, msg)

	pairBlockCancellations := dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, keepertest.TestContract, keepertest.TestPair)
	require.Nil(t, err)
	require.Equal(t, 1, len(pairBlockCancellations.Get()))
	require.Equal(t, uint64(1), pairBlockCancellations.Get()[0].Id)
	require.Equal(t, sdk.OneDec(), pairBlockCancellations.Get()[0].Price)
	require.Equal(t, "atom", pairBlockCancellations.Get()[0].AssetDenom)
	require.Equal(t, "usdc", pairBlockCancellations.Get()[0].PriceDenom)
	require.Equal(t, keepertest.TestAccount, pairBlockCancellations.Get()[0].Creator)
	require.Equal(t, keepertest.TestContract, pairBlockCancellations.Get()[0].ContractAddr)
}

func TestInvalidCancels(t *testing.T) {
	// nil cancel price
	keeper, ctx := keepertest.DexKeeper(t)
	msg := &types.MsgCancelOrders{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
	}
	keeper.AddRegisteredPair(ctx, keepertest.TestContract, keepertest.TestPair)
	keeper.SetPriceTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.PriceTicksize)
	keeper.SetQuantityTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.QuantityTicksize)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	_, err := server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// nil creator
	msg = &types.MsgCancelOrders{
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
				Price:             sdk.OneDec(),
			},
		},
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// invalid creator address
	msg = &types.MsgCancelOrders{
		Creator:      "invalid",
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
				Price:             sdk.OneDec(),
			},
		},
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// nil contract address
	msg = &types.MsgCancelOrders{
		Creator: keepertest.TestAccount,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec(),
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// invalid contract address
	msg = &types.MsgCancelOrders{
		Creator: keepertest.TestAccount,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec(),
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
		ContractAddr: "invalid",
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// nil price denom
	msg = &types.MsgCancelOrders{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec(),
				PositionDirection: types.PositionDirection_LONG,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// nil asset denom
	msg = &types.MsgCancelOrders{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec(),
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				Id:                1,
			},
		},
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)

	// negative price
	msg = &types.MsgCancelOrders{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec().Neg(),
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
	}
	_, err = server.CancelOrders(wctx, msg)
	require.NotNil(t, err)
}
