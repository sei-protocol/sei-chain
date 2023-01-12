package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestOrderPrefix(t *testing.T) {
	testContract := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	address, _ := sdk.AccAddressFromBech32(testContract)
	expected := append([]byte(types.OrderKey), address.Bytes()...)
	require.Equal(t, expected, types.OrderPrefix(testContract))
}

func TestPricePrefix(t *testing.T) {
	testContract := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	testPriceDenom := "SEI"
	testAssetDenom := "ATOM"
	address, _ := sdk.AccAddressFromBech32(testContract)
	priceContractBytes := append([]byte(types.PriceKey), address.Bytes()...)
	pairBytes := types.PairPrefix(testPriceDenom, testAssetDenom)
	expectedKey := append(priceContractBytes, pairBytes...)
	require.Equal(t, expectedKey, types.PricePrefix(testContract, testPriceDenom, testAssetDenom))
}

func TestTriggerOrderBookPrefix(t *testing.T) {
	testContract := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	testPriceDenom := "SEI"
	testAssetDenom := "ATOM"
	address, _ := sdk.AccAddressFromBech32(testContract)

	triggerBookContractBytes := append([]byte(types.TriggerBookKey), address.Bytes()...)
	pairBytes := types.PairPrefix(testPriceDenom, testAssetDenom)
	expectedKey := append(triggerBookContractBytes, pairBytes...)

	require.Equal(t, expectedKey, types.TriggerOrderBookPrefix(testContract, testPriceDenom, testAssetDenom))
}
