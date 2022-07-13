package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetOrderById(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	// active order
	keeper.AddNewOrder(ctx, types.Order{
		Id:           1,
		Account:      TEST_ACCOUNT,
		ContractAddr: TEST_CONTRACT,
		PriceDenom:   TEST_PRICE_DENOM,
		AssetDenom:   TEST_ASSET_DENOM,
		Status:       types.OrderStatus_PLACED,
		Quantity:     sdk.MustNewDecFromStr("2"),
	})
	query := types.QueryGetOrderByIDRequest{
		ContractAddr: TEST_CONTRACT,
		PriceDenom:   TEST_PRICE_DENOM,
		AssetDenom:   TEST_ASSET_DENOM,
		Id:           1,
	}
	resp, err := keeper.GetOrderByID(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, uint64(1), resp.Order.Id)
	require.Equal(t, types.OrderStatus_PLACED, resp.Order.Status)

	// settled order
	keeper.UpdateOrderStatus(ctx, TEST_CONTRACT, 1, types.OrderStatus_FULFILLED)
	keeper.SetSettlements(ctx, TEST_CONTRACT, TEST_PRICE_DENOM, TEST_ASSET_DENOM, types.Settlements{
		Entries: []*types.SettlementEntry{
			{
				OrderId:  1,
				Quantity: sdk.MustNewDecFromStr("2"),
			},
		},
	})
	resp, err = keeper.GetOrderByID(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, uint64(1), resp.Order.Id)
	require.Equal(t, types.OrderStatus_FULFILLED, resp.Order.Status)

	// cancelled order
	keeper.UpdateOrderStatus(ctx, TEST_CONTRACT, 1, types.OrderStatus_CANCELLED)
	keeper.AddCancel(ctx, TEST_CONTRACT, types.Cancellation{Id: 1})
	resp, err = keeper.GetOrderByID(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, uint64(1), resp.Order.Id)
	require.Equal(t, types.OrderStatus_CANCELLED, resp.Order.Status)
}

func TestGetOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	// active order
	keeper.AddNewOrder(ctx, types.Order{
		Id:           1,
		Account:      TEST_ACCOUNT,
		ContractAddr: TEST_CONTRACT,
		PriceDenom:   TEST_PRICE_DENOM,
		AssetDenom:   TEST_ASSET_DENOM,
		Status:       types.OrderStatus_PLACED,
		Quantity:     sdk.MustNewDecFromStr("2"),
	})
	query := types.QueryGetOrdersRequest{
		ContractAddr: TEST_CONTRACT,
		Account:      TEST_ACCOUNT,
	}
	resp, err := keeper.GetOrders(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, 1, len(resp.Orders))
}
