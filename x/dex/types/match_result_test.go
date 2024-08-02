package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMatchResultSorted(t *testing.T) {
	// Test stable sorting
	order1 := &types.Order{
		Id: 1,
	}
	order2 := &types.Order{
		Id: 1,
	}
	order3 := &types.Order{
		Id: 2,
	}

	// Test sort on different field
	cancellation1 := &types.Cancellation{
		Id:         1,
		AssetDenom: "a",
	}
	cancellation2 := &types.Cancellation{
		Id:         1,
		AssetDenom: "b",
	}

	// Test sort on string
	settlement1 := &types.SettlementEntry{
		Account: "a",
	}
	settlement2 := &types.SettlementEntry{
		Account: "b",
	}

	orders := []*types.Order{order3, order1, order2}
	cancellations := []*types.Cancellation{cancellation2, cancellation1}
	settlements := []*types.SettlementEntry{settlement2, settlement1}

	matchResult := types.NewMatchResult(orders, cancellations, settlements)
	expectedOrders := []*types.Order{order1, order2, order3}
	expectedCancellations := []*types.Cancellation{cancellation1, cancellation2}
	expectedSettlements := []*types.SettlementEntry{settlement1, settlement2}
	require.Equal(t, expectedOrders, matchResult.Orders)
	// Check actual elements, since slice match above don't seem to capture if the orders point to different objects
	require.True(t, order1 == matchResult.Orders[0])
	require.True(t, order2 == matchResult.Orders[1])
	require.Equal(t, expectedCancellations, matchResult.Cancellations)
	require.Equal(t, expectedSettlements, matchResult.Settlements)
}
