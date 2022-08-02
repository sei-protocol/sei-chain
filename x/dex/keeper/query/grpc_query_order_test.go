package query_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetOrderById(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	wctx := sdk.WrapSDKContext(ctx)
	// active order
	keeper.AddNewOrder(ctx, types.Order{
		Id:           1,
		Account:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		PriceDenom:   keepertest.TestPriceDenom,
		AssetDenom:   keepertest.TestAssetDenom,
		Status:       types.OrderStatus_PLACED,
		Quantity:     sdk.MustNewDecFromStr("2"),
	})
	query := types.QueryGetOrderByIDRequest{
		ContractAddr: keepertest.TestContract,
		PriceDenom:   keepertest.TestPriceDenom,
		AssetDenom:   keepertest.TestAssetDenom,
		Id:           1,
	}
	resp, err := wrapper.GetOrder(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, uint64(1), resp.Order.Id)
	require.Equal(t, types.OrderStatus_PLACED, resp.Order.Status)

	// settled order
	keeper.UpdateOrderStatus(ctx, keepertest.TestContract, 1, types.OrderStatus_FULFILLED)
	keeper.SetSettlements(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, types.Settlements{
		Entries: []*types.SettlementEntry{
			{
				OrderId:  1,
				Quantity: sdk.MustNewDecFromStr("2"),
			},
		},
	})
	resp, err = wrapper.GetOrder(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, uint64(1), resp.Order.Id)
	require.Equal(t, types.OrderStatus_FULFILLED, resp.Order.Status)

	// cancelled order
	keeper.UpdateOrderStatus(ctx, keepertest.TestContract, 1, types.OrderStatus_CANCELLED)
	keeper.AddCancel(ctx, keepertest.TestContract, &types.Cancellation{Id: 1})
	resp, err = wrapper.GetOrder(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, uint64(1), resp.Order.Id)
	require.Equal(t, types.OrderStatus_CANCELLED, resp.Order.Status)
}

func TestGetOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	wctx := sdk.WrapSDKContext(ctx)
	// active order
	keeper.AddNewOrder(ctx, types.Order{
		Id:           1,
		Account:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		PriceDenom:   keepertest.TestPriceDenom,
		AssetDenom:   keepertest.TestAssetDenom,
		Status:       types.OrderStatus_PLACED,
		Quantity:     sdk.MustNewDecFromStr("2"),
	})
	query := types.QueryGetOrdersRequest{
		ContractAddr: keepertest.TestContract,
		Account:      keepertest.TestAccount,
	}
	resp, err := wrapper.GetOrders(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, 1, len(resp.Orders))
}
