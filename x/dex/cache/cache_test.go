package dex_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"github.com/stretchr/testify/require"
)

const (
	TEST_CONTRACT = "test"
	TEST_PAIR     = "pair"
)

func TestDeepCopy(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateTwo := stateOne.DeepCopy()
	stateTwo.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           2,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	// old state must not be changed
	require.Equal(t, 1, len(*stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR))))
	// new state must be changed
	require.Equal(t, 2, len(*stateTwo.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR))))
}

func TestDeepFilterAccounts(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           2,
		Account:      "test2",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddCancel(types.Cancellation{
		Id:      1,
		Creator: "test",
	})
	stateOne.GetBlockCancels(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddCancel(types.Cancellation{
		Id:      2,
		Creator: "test2",
	})
	stateOne.GetDepositInfo(utils.ContractAddress(TEST_CONTRACT)).AddDeposit(dex.DepositInfoEntry{
		Creator: "test",
	})
	stateOne.GetDepositInfo(utils.ContractAddress(TEST_CONTRACT)).AddDeposit(dex.DepositInfoEntry{
		Creator: "test2",
	})
	stateOne.GetLiquidationRequests(utils.ContractAddress(TEST_CONTRACT)).AddNewLiquidationRequest("test", "")
	stateOne.GetLiquidationRequests(utils.ContractAddress(TEST_CONTRACT)).AddNewLiquidationRequest("test2", "")

	stateOne.DeepFilterAccount("test")
	require.Equal(t, 1, len(stateOne.BlockOrders))
	require.Equal(t, 1, len(stateOne.BlockCancels))
	require.Equal(t, 1, len(stateOne.DepositInfo))
	require.Equal(t, 1, len(stateOne.LiquidationRequests))
}

func TestClear(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.Clear()
	require.Equal(t, 0, len(*stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR))))
}

func TestMarkFailedToPlaceByAccounts(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).MarkFailedToPlaceByAccounts([]string{"test"})
	require.Equal(t, types.OrderStatus_FAILED_TO_PLACE,
		(*stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)))[0].Status)
}

func TestMarkFailedToPlace(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	unsuccessfulOrder := wasm.UnsuccessfulOrder{
		ID:     1,
		Reason: "some reason",
	}
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).MarkFailedToPlace([]wasm.UnsuccessfulOrder{unsuccessfulOrder})
	require.Equal(t, types.OrderStatus_FAILED_TO_PLACE,
		(*stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)))[0].Status)
	require.Equal(t, "some reason",
		(*stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)))[0].StatusDescription)
}

func TestGetSortedMarketOrders(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                1,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIQUIDATION,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                2,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                3,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                4,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIQUIDATION,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                5,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("80"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                6,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                7,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).AddOrder(types.Order{
		Id:                8,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})

	marketBuysWithLiquidation := stateOne.GetBlockOrders(
		utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_LONG, true,
	)
	require.Equal(t, uint64(3), marketBuysWithLiquidation[0].Id)
	require.Equal(t, uint64(1), marketBuysWithLiquidation[1].Id)
	require.Equal(t, uint64(2), marketBuysWithLiquidation[2].Id)

	marketBuysWithoutLiquidation := stateOne.GetBlockOrders(
		utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_LONG, false,
	)
	require.Equal(t, uint64(3), marketBuysWithoutLiquidation[0].Id)
	require.Equal(t, uint64(2), marketBuysWithoutLiquidation[1].Id)

	marketSellsWithLiquidation := stateOne.GetBlockOrders(
		utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_SHORT, true,
	)
	require.Equal(t, uint64(6), marketSellsWithLiquidation[0].Id)
	require.Equal(t, uint64(5), marketSellsWithLiquidation[1].Id)
	require.Equal(t, uint64(4), marketSellsWithLiquidation[2].Id)

	marketSellsWithoutLiquidation := stateOne.GetBlockOrders(
		utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_SHORT, false,
	)
	require.Equal(t, uint64(6), marketSellsWithoutLiquidation[0].Id)
	require.Equal(t, uint64(5), marketSellsWithoutLiquidation[1].Id)
}
