package types

import (
	"crypto/sha256"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func TestTxHashFromProtoValidatesLength(t *testing.T) {
	testCases := []struct {
		name string
		size int
	}{
		{name: "empty", size: 0},
		{name: "short", size: sha256.Size - 1},
		{name: "long", size: sha256.Size + 1},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TxHashFromProto(&tmproto.TxKey{TxKey: make([]byte, tc.size)})
			require.Error(t, err)
		})
	}

	_, err := TxHashFromProto(nil)
	require.Error(t, err)

	key := make([]byte, sha256.Size)
	for i := range key {
		key[i] = byte(i)
	}
	hash, err := TxHashFromProto(&tmproto.TxKey{TxKey: key})
	require.NoError(t, err)
	require.Equal(t, TxHash(key), hash)
}
