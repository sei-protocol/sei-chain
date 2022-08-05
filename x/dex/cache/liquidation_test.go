package dex_test

import (
	"testing"

	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/stretchr/testify/require"
)

func TestLiquidationFilterByAccount(t *testing.T) {
	liquidations := dex.NewLiquidationRequests()
	liquidation := dex.LiquidationRequest{
		Requestor: "abc",
	}
	liquidations.Add(&liquidation)
	liquidations.FilterByAccount("abc")
	require.Equal(t, 0, len(liquidations.Get()))
}

func TestIsAccountLiquidating(t *testing.T) {
	liquidations := dex.NewLiquidationRequests()
	liquidation := dex.LiquidationRequest{
		Requestor:          "abc",
		AccountToLiquidate: "def",
	}
	liquidations.Add(&liquidation)
	require.True(t, liquidations.IsAccountLiquidating("def"))
	require.False(t, liquidations.IsAccountLiquidating("abc"))
}
