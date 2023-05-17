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
}

func TestGetOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	wctx := sdk.WrapSDKContext(ctx)
	// active order
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

	query := types.QueryGetOrdersRequest{
		ContractAddr: keepertest.TestContract,
		Account:      keepertest.TestAccount,
	}
	resp, err := wrapper.GetOrders(wctx, &query)
	require.Nil(t, err)
	require.Equal(t, 1, len(resp.Orders))
}
