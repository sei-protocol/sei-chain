package types_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
)

func TestGetValidatorSlashEventHeight(t *testing.T) {
	valAddr := sdk.ValAddress([]byte("validator-address-01"))
	prefix := types.GetValidatorSlashEventPrefix(valAddr)

	for _, tc := range []struct {
		height uint64
		period uint64
	}{
		{0, 0},
		{2, 3},
		{1000, 7},
		{math.MaxUint64, 1},
	} {
		key := types.GetValidatorSlashEventKey(valAddr, tc.height, tc.period)
		require.Equal(t, prefix, key[:len(prefix)])
		require.Equal(t, tc.height, types.GetValidatorSlashEventHeight(key[len(prefix):]))
	}

	require.Panics(t, func() { types.GetValidatorSlashEventHeight([]byte{1, 2, 3}) })
}
