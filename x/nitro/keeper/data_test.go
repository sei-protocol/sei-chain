package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/nitro/keeper"
	"github.com/stretchr/testify/require"
)

func TestDataEncodingDecoding(t *testing.T) {
	data := [][]byte{
		[]byte("abc"),
		[]byte("defghj"),
		[]byte("z"),
	}
	encoded := keeper.EncodeTransactionData(data)
	require.Equal(t, 42, len(encoded))

	decoded, err := keeper.DecodeTransactionData(encoded)
	require.Nil(t, err)
	require.Equal(t, 3, len(decoded))
	require.Equal(t, "abc", string(decoded[0]))
	require.Equal(t, "defghj", string(decoded[1]))
	require.Equal(t, "z", string(decoded[2]))
}
