package state_test

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestSplitUseiWeiAmount(t *testing.T) {
	for _, test := range []struct {
		amt         *big.Int
		expectedSei *big.Int
		expectedWei *big.Int
	}{
		{
			amt:         big.NewInt(0),
			expectedSei: big.NewInt(0),
			expectedWei: big.NewInt(0),
		}, {
			amt:         big.NewInt(1),
			expectedSei: big.NewInt(0),
			expectedWei: big.NewInt(1),
		}, {
			amt:         big.NewInt(999_999_999_999),
			expectedSei: big.NewInt(0),
			expectedWei: big.NewInt(999_999_999_999),
		}, {
			amt:         big.NewInt(1_000_000_000_000),
			expectedSei: big.NewInt(1),
			expectedWei: big.NewInt(0),
		}, {
			amt:         big.NewInt(1_000_000_000_001),
			expectedSei: big.NewInt(1),
			expectedWei: big.NewInt(1),
		}, {
			amt:         big.NewInt(123_456_789_123_456_789),
			expectedSei: big.NewInt(123456),
			expectedWei: big.NewInt(789_123_456_789),
		},
	} {
		usei, wei := state.SplitUseiWeiAmount(test.amt)
		require.Equal(t, test.expectedSei, usei)
		require.Equal(t, test.expectedWei, wei)
	}
}
