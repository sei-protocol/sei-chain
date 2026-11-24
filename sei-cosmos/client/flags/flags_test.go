package flags_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

func TestParseGasSetting(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		expected  uint64
		expectErr bool
	}{
		{"empty input", "", flags.DefaultGasLimit, false},
		{"auto", "auto", 0, true},
		{"valid custom gas", "73800", 73800, false},
		{"invalid custom gas", "-73800", 0, true},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			gas, err := flags.ParseGasSetting(tc.input)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, gas)
			}
		})
	}
}
