package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCosmosGasParams(t *testing.T) {
	// Verify validation: numerator and denominator != 0
	invalidCosmosGasParam := NewCosmosGasParams(0, 1)
	err := invalidCosmosGasParam.Validate()
	require.NotNil(t, err)

	invalidCosmosGasParam = NewCosmosGasParams(1, 0)
	err = invalidCosmosGasParam.Validate()
	require.NotNil(t, err)
}
