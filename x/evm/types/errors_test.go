package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAssociationMissingErr(t *testing.T) {
	tests := []struct {
		name            string
		address         string
		wantError       string
		wantAddressType string
	}{
		{
			name:            "EVM address",
			address:         "0x1234567890abcdef",
			wantError:       "address 0x1234567890abcdef is not linked",
			wantAddressType: "evm",
		},
		{
			name:            "SEI address",
			address:         "sei1234567890abcdef",
			wantError:       "address sei1234567890abcdef is not linked",
			wantAddressType: "sei",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.NewAssociationMissingErr(tt.address)

			// Test Error method
			require.Equal(t, tt.wantError, err.Error())

			// Test AddressType method
			require.Equal(t, tt.wantAddressType, err.AddressType())
		})
	}
}
