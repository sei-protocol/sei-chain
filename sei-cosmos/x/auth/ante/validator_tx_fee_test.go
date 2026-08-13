package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"
)

func TestGetMinimumGasWanted(t *testing.T) {
	// Test case 1: Both global and validator minimum gas prices are empty.
	globalMinGasPrices := sdk.NewDecCoins()
	validatorMinGasPrices := sdk.NewDecCoins()

	minGasWanted := ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted := sdk.NewDecCoins()

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))

	// Test case 2: Global minimum gas prices is empty, validator minimum gas prices is not empty.
	globalMinGasPrices = sdk.NewDecCoins()
	validatorMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)))

	minGasWanted = ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)))

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))

	// Test case 3: Global minimum gas prices is not empty, validator minimum gas prices is empty.
	globalMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("bar", sdk.NewInt(2)))
	validatorMinGasPrices = sdk.NewDecCoins()

	minGasWanted = ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted = sdk.NewDecCoins(sdk.NewDecCoin("bar", sdk.NewInt(2)))

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))

	// Test case 4: Global minimum gas prices and validator minimum gas prices have overlapping coins.
	globalMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(2)))
	validatorMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("bar", sdk.NewInt(3)), sdk.NewDecCoin("baz", sdk.NewInt(4)))

	minGasWanted = ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(3)), sdk.NewDecCoin("baz", sdk.NewInt(4)))

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))

	// Test case 5: Global minimum gas prices and validator minimum gas prices have no overlapping coins.
	globalMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(2)))
	validatorMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("baz", sdk.NewInt(3)), sdk.NewDecCoin("qux", sdk.NewInt(4)))

	minGasWanted = ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(2)), sdk.NewDecCoin("baz", sdk.NewInt(3)), sdk.NewDecCoin("qux", sdk.NewInt(4)))

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))

	// Test case 6: Global minimum gas prices and validator minimum gas prices have the same coins but different amounts.
	globalMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(2)))
	validatorMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(3)), sdk.NewDecCoin("bar", sdk.NewInt(4)))

	minGasWanted = ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(3)), sdk.NewDecCoin("bar", sdk.NewInt(4)))

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))

	// Test case 7: Global minimum gas prices and validator minimum gas prices have different coins.
	globalMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(2)))
	validatorMinGasPrices = sdk.NewDecCoins(sdk.NewDecCoin("baz", sdk.NewInt(3)), sdk.NewDecCoin("qux", sdk.NewInt(4)))

	minGasWanted = ante.GetMinimumGasPricesWantedSorted(globalMinGasPrices, validatorMinGasPrices)

	expectedMinGasWanted = sdk.NewDecCoins(sdk.NewDecCoin("foo", sdk.NewInt(1)), sdk.NewDecCoin("bar", sdk.NewInt(2)), sdk.NewDecCoin("baz", sdk.NewInt(3)), sdk.NewDecCoin("qux", sdk.NewInt(4)))

	require.True(t, expectedMinGasWanted.IsEqual(minGasWanted))
}

func TestGetTxPriority(t *testing.T) {
	sdk.RegisterDenom("test", sdk.NewDecWithPrec(1, 6))
	require.Equal(
		t,
		int64(0),
		ante.GetTxPriority(sdk.NewCoins(), 1000),
	)
	require.Equal(
		t,
		int64(1_000_000_000),
		ante.GetTxPriority(sdk.NewCoins(sdk.NewCoin("test", sdk.NewInt(1))), 1000),
	)
	require.Equal(
		t,
		int64(0),
		ante.GetTxPriority(sdk.NewCoins(sdk.NewCoin("test", sdk.NewInt(1))), 10_000_000_000_000), // gas too large
	)
}
