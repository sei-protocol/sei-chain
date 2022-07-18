package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestOrderPrefix(t *testing.T) {
	testContract := "test"
	expected := append([]byte(types.OrderKey), []byte(testContract)...)
	require.Equal(t, expected, types.OrderPrefix(testContract))
}

func TestPricePrefix(t *testing.T) {
	testContract := "test"
	testPriceDenom := "SEI"
	testAssetDenom := "ATOM"
	priceContractBytes := append([]byte(types.PriceKey), []byte(testContract)...)
	pairBytes := append([]byte(testPriceDenom), []byte(testAssetDenom)...)
	expectedKey := append(priceContractBytes, pairBytes...)
	require.Equal(t, expectedKey, types.PricePrefix(testContract, testPriceDenom, testAssetDenom))
}
