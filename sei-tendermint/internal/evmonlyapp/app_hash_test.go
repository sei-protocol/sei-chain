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
		{pairs: 0, want: "0x98c012bb0ffae50b5f9969e668577c9449f0aa53d728cfba607162470a07f7cb"},
		{pairs: 1, want: "0x787e0c740324fd0811925ded7d865e976eb7c59c1043bf89bf4644e221e2f259"},
		{pairs: 64, want: "0xc6cbf658ce324a5e210da5d7fd32d24239d3471a2716d4343d7096949c25b0ed"},
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

func TestDummyAppHashLeavesStateChangesOut(t *testing.T) {
	previous := common.HexToHash("0x01")
	blockHash := common.HexToHash("0x02")
	withChange := &evmonly.BlockResult{GasUsed: 21_000, ChangeSet: evmonly.StateChangeSet{
		Accounts: []evmonly.AccountChange{{Address: common.HexToAddress("0x03"), Account: evmonly.AccountState{Balance: big.NewInt(1)}}},
	}}
	without := &evmonly.BlockResult{GasUsed: 21_000}

	full, err := hashEVMOnlyResult(previous, 7, blockHash, withChange)
	require.NoError(t, err)
	fullEmpty, err := hashEVMOnlyResult(previous, 7, blockHash, without)
	require.NoError(t, err)
	require.NotEqual(t, full, fullEmpty)

	DummyAppHash = true
	t.Cleanup(func() { DummyAppHash = false })
	dummy, err := hashEVMOnlyResult(previous, 7, blockHash, withChange)
	require.NoError(t, err)
	dummyEmpty, err := hashEVMOnlyResult(previous, 7, blockHash, without)
	require.NoError(t, err)
	require.Equal(t, dummy, dummyEmpty)
}
