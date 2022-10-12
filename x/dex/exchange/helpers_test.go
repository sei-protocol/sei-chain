package exchange_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMergeExecutionOutcomes(t *testing.T) {
	s1 := types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	}

	s2 := types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	}

	e1 := exchange.ExecutionOutcome{
		TotalNotional: sdk.MustNewDecFromStr("1"),
		TotalQuantity: sdk.MustNewDecFromStr("2"),
		Settlements:   []*types.SettlementEntry{&s1, &s2, &s1},
		MinPrice:      sdk.MustNewDecFromStr("1"),
		MaxPrice:      sdk.MustNewDecFromStr("4"),
	}

	e2 := exchange.ExecutionOutcome{
		TotalNotional: sdk.MustNewDecFromStr("4"),
		TotalQuantity: sdk.MustNewDecFromStr("4"),
		Settlements:   []*types.SettlementEntry{&s1, &s2},
		MinPrice:      sdk.MustNewDecFromStr("0.5"),
		MaxPrice:      sdk.MustNewDecFromStr("3"),
	}

	outcome := e1.Merge(&e2)

	require.Equal(t, outcome.TotalNotional, sdk.MustNewDecFromStr("5"))
	require.Equal(t, outcome.TotalQuantity, sdk.MustNewDecFromStr("6"))
	require.Equal(t, len(outcome.Settlements), 5)
	require.Equal(t, outcome.MinPrice, sdk.MustNewDecFromStr("0.5"))
	require.Equal(t, outcome.MaxPrice, sdk.MustNewDecFromStr("4"))
}
