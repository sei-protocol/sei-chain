package types_test

import (
	"testing"

	"github.com/cosmos/ibc-go/v2/modules/core/03-connection/types"
	"github.com/stretchr/testify/require"
)

func TestValidateParams(t *testing.T) {
	testCases := []struct {
		name    string
		params  types.Params
		expPass bool
	}{
		{"default params", types.DefaultParams(), true},
		{"custom params", types.NewParams(10), true},
		{"blank client", types.NewParams(0), false},
	}

	for _, tc := range testCases {
		err := tc.params.Validate()
		if tc.expPass {
			require.NoError(t, err, tc.name)
		} else {
			require.Error(t, err, tc.name)
		}
	}
}
