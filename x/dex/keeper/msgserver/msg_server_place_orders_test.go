package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const (
	TestCreator  = "sei1ewxvf5a9wq9zk5nurtl6m9yfxpnhyp7s7uk5sl"
	TestContract = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
)

func TestPlaceOrder(t *testing.T) {
	msg := &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("10"),
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
				Account:           "testaccount",
			},
			{
				Price:             sdk.MustNewDecFromStr("20"),
				Quantity:          sdk.MustNewDecFromStr("5"),
				Data:              "",
				PositionDirection: types.PositionDirection_SHORT,
				OrderType:         types.OrderType_MARKET,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
				Account:           "testaccount",
			},
		},
	}
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, TestContract, keepertest.TestPair)
	keeper.SetTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.Ticksize)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper, nil)
	res, err := server.PlaceOrders(wctx, msg)
	require.Nil(t, err)
	require.Equal(t, 2, len(res.OrderIds))
	require.Equal(t, uint64(0), res.OrderIds[0])
	require.Equal(t, uint64(1), res.OrderIds[1])
}

func TestPlaceInvalidOrder(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, TestContract, keepertest.TestPair)
	keeper.SetTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.Ticksize)
	wctx := sdk.WrapSDKContext(ctx)

	// Empty quantity
	msg := &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("10"),
				Quantity:          sdk.Dec{},
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
				Account:           "testaccount",
			},
		},
	}
	server := msgserver.NewMsgServerImpl(*keeper, nil)
	_, err := server.PlaceOrders(wctx, msg)
	require.NotNil(t, err)

	// Empty price
	msg = &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.Dec{},
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
				Account:           "testaccount",
			},
		},
	}
	server = msgserver.NewMsgServerImpl(*keeper, nil)
	_, err = server.PlaceOrders(wctx, msg)
	require.NotNil(t, err)

	// Negative quantity
	msg = &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("10"),
				Quantity:          sdk.MustNewDecFromStr("-1"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
				Account:           "testaccount",
			},
		},
	}
	server = msgserver.NewMsgServerImpl(*keeper, nil)
	_, err = server.PlaceOrders(wctx, msg)
	require.NotNil(t, err)

	// Negative price
	msg = &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("-1"),
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
				Account:           "testaccount",
			},
		},
	}
	server = msgserver.NewMsgServerImpl(*keeper, nil)
	_, err = server.PlaceOrders(wctx, msg)
	require.NotNil(t, err)

	// Missing contract
	msg = &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("-1"),
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Account:           "testaccount",
			},
		},
	}
	server = msgserver.NewMsgServerImpl(*keeper, nil)
	_, err = server.PlaceOrders(wctx, msg)
	require.NotNil(t, err)

	// Missing account
	msg = &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("-1"),
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				ContractAddr:      TestContract,
			},
		},
	}
	server = msgserver.NewMsgServerImpl(*keeper, nil)
	_, err = server.PlaceOrders(wctx, msg)
	require.NotNil(t, err)
}
