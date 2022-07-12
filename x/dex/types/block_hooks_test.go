package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestPopulateOrderPlacementResults(t *testing.T) {
	account := "test"
	contract := "contract"
	orderToPopulate := types.Order{
		Id:                1,
		Account:           account,
		ContractAddr:      contract,
		Price:             sdk.MustNewDecFromStr("1"),
		Quantity:          sdk.MustNewDecFromStr("1"),
		PriceDenom:        "SEI",
		AssetDenom:        "ATOM",
		OrderType:         types.OrderType_LIMIT,
		PositionDirection: types.PositionDirection_LONG,
		Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
	}
	resultsMap := map[string]types.ContractOrderResult{}
	types.PopulateOrderPlacementResults(contract, []types.Order{orderToPopulate}, resultsMap)
	require.Equal(t, 1, len(resultsMap))
	require.Equal(t, contract, resultsMap[account].ContractAddr)
	require.Equal(t, 1, len(resultsMap[account].OrderPlacementResults))
	require.Equal(t, uint64(1), resultsMap[account].OrderPlacementResults[0].OrderID)
}

func TestPopulateOrderExecutionResults(t *testing.T) {
	account := "test"
	contract := "contract"
	settlement := types.SettlementEntry{
		OrderId:                1,
		Account:                account,
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("1"),
		Quantity:               sdk.MustNewDecFromStr("1"),
		PriceDenom:             "SEI",
		AssetDenom:             "ATOM",
		OrderType:              "Limit",
		PositionDirection:      "Long",
	}
	resultsMap := map[string]types.ContractOrderResult{}
	types.PopulateOrderExecutionResults(contract, []*types.SettlementEntry{&settlement}, resultsMap)
	require.Equal(t, 1, len(resultsMap))
	require.Equal(t, contract, resultsMap[account].ContractAddr)
	require.Equal(t, 1, len(resultsMap[account].OrderExecutionResults))
	require.Equal(t, uint64(1), resultsMap[account].OrderExecutionResults[0].OrderID)
}
