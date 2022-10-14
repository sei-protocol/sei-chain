package utils_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

func TestGetPairString(t *testing.T) {
	pair := types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"}
	expected := utils.PairString("USDC|ATOM")
	require.Equal(t, expected, utils.GetPairString(&pair))
}

func TestGetPriceAssetString(t *testing.T) {
	priceDenom, assetDenom := utils.GetPriceAssetString(utils.PairString("USDC|ATOM"))
	require.Equal(t, "USDC", priceDenom)
	require.Equal(t, "ATOM", assetDenom)
}
