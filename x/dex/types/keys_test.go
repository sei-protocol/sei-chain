package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestOrderPrefix(t *testing.T) {
	testContract := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	addr, _ := sdk.AccAddressFromBech32(testContract)
	addr = address.MustLengthPrefix(addr)
	expected := append([]byte(types.OrderKey), addr...)
	require.Equal(t, expected, types.OrderPrefix(testContract))
}

func TestPricePrefix(t *testing.T) {
	testContract := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	testPriceDenom := "SEI"
	testAssetDenom := "ATOM"
	address := types.AddressKeyPrefix(testContract)
	priceContractBytes := append([]byte(types.PriceKey), address...)
	pairBytes := types.PairPrefix(testPriceDenom, testAssetDenom)
	expectedKey := append(priceContractBytes, pairBytes...)
	require.Equal(t, expectedKey, types.PricePrefix(testContract, testPriceDenom, testAssetDenom))
}
