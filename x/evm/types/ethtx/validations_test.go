package ethtx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAddress(t *testing.T) {
	require.NoError(t, ValidateAddress("0x1234567890ABCDEF1234567890abcdef12345678"))
	require.NoError(t, ValidateAddress("0X1234567890ABCDEF1234567890abcdef12345678"))
	require.NoError(t, ValidateAddress("1234567890ABCDEF1234567890abcdef12345678"))

	require.Error(t, ValidateAddress("0x1234567890ABCDEF1234567890abcdef1234567"))
	require.Error(t, ValidateAddress("0x1234567890ABCDEF1234567890abcdef123456789"))
	require.Error(t, ValidateAddress("1234567890ABCDEF1234567890abcdef1234567G"))
}
