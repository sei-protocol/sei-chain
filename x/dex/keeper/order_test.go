package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const TEST_ACCOUNT = "test"

func TestAddOrder(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	order := types.Order{Id: 1, ContractAddr: TEST_CONTRACT, Account: TEST_ACCOUNT}
	keeper.AddNewOrder(ctx, order)
	writtenOrders := keeper.GetOrdersByIds(ctx, TEST_CONTRACT, []uint64{1})
	require.Equal(t, 1, len(writtenOrders))
	require.Equal(t, uint64(1), writtenOrders[1].Id)
	require.Equal(t, TEST_CONTRACT, writtenOrders[1].ContractAddr)
	require.Equal(t, TEST_ACCOUNT, writtenOrders[1].Account)
	require.Equal(t, 1, len(keeper.GetAccountActiveOrders(ctx, TEST_CONTRACT, TEST_ACCOUNT).Ids))
}

func TestAddCancel(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	order := types.Order{Id: 1, ContractAddr: TEST_CONTRACT, Account: TEST_ACCOUNT}
	keeper.AddNewOrder(ctx, order)
	keeper.AddCancel(ctx, TEST_CONTRACT, types.Cancellation{Id: 1})
	// The old order should NOT be deleted (serves as a permenant record)
	require.Equal(t, 1, len(keeper.GetOrdersByIds(ctx, TEST_CONTRACT, []uint64{1})))
	// The active index should be updated
	require.Equal(t, 0, len(keeper.GetAccountActiveOrders(ctx, TEST_CONTRACT, TEST_ACCOUNT).Ids))
}
