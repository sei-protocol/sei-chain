package dex_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMarkFailedToPlace(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	unsuccessfulOrder := types.UnsuccessfulOrder{
		ID:     1,
		Reason: "some reason",
	}
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).MarkFailedToPlace([]types.UnsuccessfulOrder{unsuccessfulOrder})
	require.Equal(t, types.OrderStatus_FAILED_TO_PLACE,
		stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()[0].Status)
	require.Equal(t, "some reason",
		stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()[0].StatusDescription)
}

func TestGetByID(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                1,
		Account:           "test1",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                2,
		Account:           "test2",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})

	order1 := stateOne.GetBlockOrders(
		ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).GetByID(uint64(1))
	order2 := stateOne.GetBlockOrders(
		ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).GetByID(uint64(2))
	require.Equal(t, uint64(1), order1.Id)
	require.Equal(t, uint64(2), order2.Id)
	require.Equal(t, "test1", order1.Account)
	require.Equal(t, "test2", order2.Account)
	require.Equal(t, TEST_CONTRACT, order1.ContractAddr)
	require.Equal(t, TEST_CONTRACT, order2.ContractAddr)
	require.Equal(t, types.PositionDirection_LONG, order1.PositionDirection)
	require.Equal(t, types.PositionDirection_SHORT, order2.PositionDirection)
	require.Equal(t, types.OrderType_LIMIT, order1.OrderType)
	require.Equal(t, types.OrderType_MARKET, order2.OrderType)
	require.Equal(t, sdk.MustNewDecFromStr("150"), order1.Price)
	require.Equal(t, sdk.MustNewDecFromStr("100"), order2.Price)
}

func TestGetSortedMarketOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                1,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                2,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                3,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                4,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                5,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("80"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                6,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                7,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                8,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                9,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_FOKMARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                10,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_FOKMARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                11,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_FOKMARKET,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                12,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_FOKMARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                13,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_FOKMARKET,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                14,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_FOKMARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                15,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_FOKMARKETBYVALUE,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                16,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_FOKMARKETBYVALUE,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                17,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_FOKMARKETBYVALUE,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                18,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_FOKMARKETBYVALUE,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                19,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_FOKMARKETBYVALUE,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:                20,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_FOKMARKETBYVALUE,
		Price:             sdk.MustNewDecFromStr("100"),
	})

	marketBuys := stateOne.GetBlockOrders(
		ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).GetSortedMarketOrders(types.PositionDirection_LONG)
	require.Equal(t, uint64(16), marketBuys[0].Id)
	require.Equal(t, uint64(10), marketBuys[1].Id)
	require.Equal(t, uint64(3), marketBuys[2].Id)
	require.Equal(t, uint64(1), marketBuys[3].Id)
	require.Equal(t, uint64(11), marketBuys[4].Id)
	require.Equal(t, uint64(17), marketBuys[5].Id)
	require.Equal(t, uint64(2), marketBuys[6].Id)
	require.Equal(t, uint64(9), marketBuys[7].Id)
	require.Equal(t, uint64(15), marketBuys[8].Id)

	marketSells := stateOne.GetBlockOrders(
		ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).GetSortedMarketOrders(types.PositionDirection_SHORT)
	require.Equal(t, uint64(18), marketSells[0].Id)
	require.Equal(t, uint64(12), marketSells[1].Id)
	require.Equal(t, uint64(6), marketSells[2].Id)
	require.Equal(t, uint64(5), marketSells[3].Id)
	require.Equal(t, uint64(4), marketSells[4].Id)
	require.Equal(t, uint64(14), marketSells[5].Id)
	require.Equal(t, uint64(20), marketSells[6].Id)
	require.Equal(t, uint64(13), marketSells[7].Id)
	require.Equal(t, uint64(19), marketSells[8].Id)
}
