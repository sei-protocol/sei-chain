package dex_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const TEST_CONTRACT = "test"
const TEST_PAIR = "pair"

func TestDeepCopy(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateTwo := stateOne.DeepCopy()
	stateTwo.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           2,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	// old state must not be changed
	require.Equal(t, 1, len(*stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR))))
	// new state must be changed
	require.Equal(t, 2, len(*stateTwo.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR))))
}

func TestClear(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.Clear()
	require.Equal(t, 0, len(*stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR))))
}

func TestMarkFailedToPlaceByAccounts(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).MarkFailedToPlaceByAccounts([]string{"test"})
	require.Equal(t, types.OrderStatus_FAILED_TO_PLACE,
		(*stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)))[0].Status)
}

func TestMarkFailedToPlaceByIds(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).MarkFailedToPlaceByIds([]uint64{1})
	require.Equal(t, types.OrderStatus_FAILED_TO_PLACE,
		(*stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)))[0].Status)
}

func TestGetSortedMarketOrders(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                1,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIQUIDATION,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                2,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                3,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                4,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIQUIDATION,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                5,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("80"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                6,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                7,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                8,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})

	marketBuysWithLiquidation := stateOne.GetBlockOrders(
		types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_LONG, true,
	)
	require.Equal(t, uint64(3), marketBuysWithLiquidation[0].Id)
	require.Equal(t, uint64(1), marketBuysWithLiquidation[1].Id)
	require.Equal(t, uint64(2), marketBuysWithLiquidation[2].Id)

	marketBuysWithoutLiquidation := stateOne.GetBlockOrders(
		types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_LONG, false,
	)
	require.Equal(t, uint64(3), marketBuysWithoutLiquidation[0].Id)
	require.Equal(t, uint64(2), marketBuysWithoutLiquidation[1].Id)

	marketSellsWithLiquidation := stateOne.GetBlockOrders(
		types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_SHORT, true,
	)
	require.Equal(t, uint64(6), marketSellsWithLiquidation[0].Id)
	require.Equal(t, uint64(5), marketSellsWithLiquidation[1].Id)
	require.Equal(t, uint64(4), marketSellsWithLiquidation[2].Id)

	marketSellsWithoutLiquidation := stateOne.GetBlockOrders(
		types.ContractAddress(TEST_CONTRACT), types.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_SHORT, false,
	)
	require.Equal(t, uint64(6), marketSellsWithoutLiquidation[0].Id)
	require.Equal(t, uint64(5), marketSellsWithoutLiquidation[1].Id)
}
