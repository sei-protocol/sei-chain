package evmonlyapp

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

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
		balance := uint64(i) * 1_000_000
		// Priors one above the values make both the balance and the nonce a change.
		result.ChangeSet.Accounts = append(result.ChangeSet.Accounts, evmonly.AccountChange{
			Address: addr(b),
			Account: evmonly.AccountState{Balance: new(big.Int).SetUint64(balance), Nonce: uint64(i)},
			Prior:   evmonly.AccountState{Balance: new(big.Int).SetUint64(balance + 1), Nonce: uint64(i) + 1},
		})
		result.ChangeSet.Storage = append(result.ChangeSet.Storage, evmonly.StorageChange{
			Address: addr(b), Key: hash(b), Value: hash(b ^ 0xff), Delete: i%3 == 0,
		})
	}
	result.ChangeSet.Accounts = append(result.ChangeSet.Accounts,
		evmonly.AccountChange{Address: addr(0xaa), Account: evmonly.AccountState{Code: []byte{0x60, 0x00, 0x60, 0x00, 0xf3}}},
		evmonly.AccountChange{Address: addr(0xbb), Prior: evmonly.AccountState{Code: []byte{0x60}}},
	)
	return result
}

// TestHashEVMOnlyResultGoldenVector pins the app-hash byte stream validators vote on.
func TestHashEVMOnlyResultGoldenVector(t *testing.T) {
	for _, tc := range []struct {
		pairs int
		want  string
	}{
		{pairs: 0, want: "0xe06583da80729663701e24668f669cab65799fc17888a9e191f013b9baaa0cd4"},
		{pairs: 1, want: "0x10593ba2553289a80202b01dc290f532e711be917668464deba5bdab1545e364"},
		{pairs: 64, want: "0xce504f8107f23f6d4b2db7c94c8664a0a0ccf363ec8264982d01f4698e032a95"},
	} {
		t.Run(fmt.Sprintf("pairs=%d", tc.pairs), func(t *testing.T) {
			got, err := hashEVMOnlyResult(
				common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
				4_321,
				common.HexToHash("0xf0e0d0c0b0a090807060504030201000ffeeddccbbaa99887766554433221100"),
				uint256.NewInt(100_000_000_000),
				evmOnlyHashFixture(tc.pairs),
			)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Hex())
		})
	}
}

// The app hash commits to the base fee a block ran at, even when the block is empty.
func TestHashEVMOnlyResultCoversTheBaseFee(t *testing.T) {
	hashAt := func(baseFee uint64) common.Hash {
		got, err := hashEVMOnlyResult(common.Hash{}, 1, common.Hash{}, uint256.NewInt(baseFee), &evmonly.BlockResult{})
		require.NoError(t, err)
		return got
	}

	require.NotEqual(t, hashAt(100_000_000_000), hashAt(100_000_000_001))
}
