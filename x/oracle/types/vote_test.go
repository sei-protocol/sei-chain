package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseExchangeRateTuples(t *testing.T) {
	valid := "123.0usei,123.123ukrw"
	_, err := ParseExchangeRateTuples(valid)
	require.NoError(t, err)

	duplicatedDenom := "100.0usei,123.123ukrw,121233.123ukrw"
	_, err = ParseExchangeRateTuples(duplicatedDenom)
	require.Error(t, err)

	invalidCoins := "123.123"
	_, err = ParseExchangeRateTuples(invalidCoins)
	require.Error(t, err)

	invalidCoinsWithValid := "123.0usei,123.1"
	_, err = ParseExchangeRateTuples(invalidCoinsWithValid)
	require.Error(t, err)

	abstainCoinsWithValid := "0.0usei,123.1ukrw"
	_, err = ParseExchangeRateTuples(abstainCoinsWithValid)
	require.NoError(t, err)
}
