package evmonlyapp

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// evmOnlyHashFixture builds a changeset touching every field hashEVMOnlyResult walks.
func evmOnlyHashFixture(pairs int) *evmonly.BlockResult {
	addr := func(b byte) common.Address {
		var a common.Address
		for i := range a {
			a[i] = b
		}
		return a
	}
	hash := func(b byte) common.Hash {
		var h common.Hash
		for i := range h {
			h[i] = b
		}
		return h
	}
	result := &evmonly.BlockResult{GasUsed: 21_000 * uint64(pairs)}
	for i := range pairs {
		b := byte(i%251 + 1)
		result.ChangeSet.Balances = append(result.ChangeSet.Balances, evmonly.BalanceChange{
			Address: addr(b), Balance: new(big.Int).SetUint64(uint64(i) * 1_000_000),
		})
		result.ChangeSet.Nonces = append(result.ChangeSet.Nonces, evmonly.NonceChange{
			Address: addr(b), Nonce: uint64(i),
		})
		result.ChangeSet.Storage = append(result.ChangeSet.Storage, evmonly.StorageChange{
			Address: addr(b), Key: hash(b), Value: hash(b ^ 0xff), Delete: i%3 == 0,
		})
	}
	result.ChangeSet.Code = append(result.ChangeSet.Code,
		evmonly.CodeChange{Address: addr(0xaa), Code: []byte{0x60, 0x00, 0x60, 0x00, 0xf3}},
		evmonly.CodeChange{Address: addr(0xbb), Delete: true},
	)
	result.ChangeSet.StorageClears = append(result.ChangeSet.StorageClears, addr(0xcc))
	return result
}

// TestHashEVMOnlyResultGoldenVector pins the app-hash byte stream validators vote on.
func TestHashEVMOnlyResultGoldenVector(t *testing.T) {
	for _, tc := range []struct {
		pairs int
		want  string
	}{
		{pairs: 0, want: "0x940d67c7f137ce6f5dff5d30287ec13d1e04a81a779fd936663244d78c54c659"},
		{pairs: 1, want: "0xeff7e68a8061850aff3232b253573e045cb7d07303a0606ee5a9af8b55623289"},
		{pairs: 64, want: "0x8e58f705e0452fc3789eb90e5fe69f29ea149cb92dbcf660e497cd41d2286d62"},
	} {
		t.Run(fmt.Sprintf("pairs=%d", tc.pairs), func(t *testing.T) {
			got, err := hashEVMOnlyResult(
				common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
				4_321,
				common.HexToHash("0xf0e0d0c0b0a090807060504030201000ffeeddccbbaa99887766554433221100"),
				evmOnlyHashFixture(tc.pairs),
			)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Hex())
		})
	}
}
