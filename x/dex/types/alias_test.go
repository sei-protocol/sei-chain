package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetPairString(t *testing.T) {
	pair := types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"}
	expected := types.PairString("USDC|ATOM")
	require.Equal(t, expected, types.GetPairString(&pair))
}

func TestGetPriceAssetString(t *testing.T) {
	priceDenom, assetDenom := types.GetPriceAssetString(types.PairString("USDC|ATOM"))
	require.Equal(t, "USDC", priceDenom)
	require.Equal(t, "ATOM", assetDenom)
}
