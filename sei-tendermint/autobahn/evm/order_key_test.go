package evm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestNTipHashCmp(t *testing.T) {
	zero := uint256.Int{}
	one := *uint256.NewInt(1)
	maxTip := *new(uint256.Int).SetAllOne()
	adjacentMaxTip := *new(uint256.Int).Sub(&maxTip, &one)
	randomTip := *uint256.MustFromHex("0x9f32e7b40c61a8d21b6e94af7053c8de12a9b6f403d75e810ca26bd984f15063")
	otherRandomTip := *uint256.MustFromHex("0x426d91a7e05b3c8f129a64de8372f05cb91e6a4d7038c25f16b09ed47a832c65")
	adjacentRandomTip := *uint256.MustFromHex("0x9f32e7b40c61a8d21b6e94af7053c8de12a9b6f403d75e810ca26bd984f15064")
	randomHash := common.HexToHash("0x0b75e39a41c628dff138a0679de52bc486f913a20d7c5e684ab092f361de8475")
	otherRandomHash := common.HexToHash("0xe1428bd076a9c351f8206e4db37a905c14f86de2a07b39c5658e104fa2d793bc")
	maxHash := common.BigToHash(maxTip.ToBig())

	zeroKey := newNTipHash(zero, common.Hash{})
	maxKey := newNTipHash(maxTip, common.Hash{})
	randomKey := newNTipHash(randomTip, randomHash)
	oneHighHashKey := newNTipHash(one, maxHash)
	maxHighHashKey := newNTipHash(maxTip, maxHash)
	adjacentMaxKey := newNTipHash(adjacentMaxTip, common.Hash{})
	randomHighHashKey := newNTipHash(randomTip, maxHash)
	otherRandomKey := newNTipHash(otherRandomTip, common.Hash{})
	randomZeroHashKey := newNTipHash(randomTip, common.Hash{})
	adjacentRandomHighHashKey := newNTipHash(adjacentRandomTip, maxHash)
	otherHashKey := newNTipHash(randomTip, otherRandomHash)
	zeroMaxHashKey := newNTipHash(zero, maxHash)

	for _, tc := range []struct {
		name string
		a, b nTipHash
		want int
	}{
		{"zero equality", zeroKey, zeroKey, 0},
		{"maximum equality", maxKey, maxKey, 0},
		{"random equality", randomKey, randomKey, 0},
		{"zero versus one", zeroKey, oneHighHashKey, 1},
		{"zero versus maximum", zeroKey, maxKey, 1},
		{"maximum versus adjacent tip", maxHighHashKey, adjacentMaxKey, -1},
		{"random tip priority", randomHighHashKey, otherRandomKey, -1},
		{"random adjacent tips", randomZeroHashKey, adjacentRandomHighHashKey, 1},
		{"random hash priority", randomKey, otherHashKey, -1},
		{"hash zero versus maximum", zeroKey, zeroMaxHashKey, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.a.Cmp(tc.b))
			require.Equal(t, -tc.want, tc.b.Cmp(tc.a))
		})
	}
}

func TestNTipHashCmpPowersOfTwo(t *testing.T) {
	maxTip := *new(uint256.Int).SetAllOne()
	maxHash := common.BigToHash(maxTip.ToBig())
	for offset := range uint(256) {
		t.Run(fmt.Sprintf("2^%d", offset), func(t *testing.T) {
			power := new(big.Int).Lsh(big.NewInt(1), offset)
			below := new(big.Int).Sub(power, big.NewInt(1))

			a := newNTipHash(*uint256.MustFromBig(power), maxHash)
			b := newNTipHash(*uint256.MustFromBig(below), common.Hash{})
			require.Equal(t, -1, a.Cmp(b))
			require.Equal(t, 1, b.Cmp(a))

			a = newNTipHash(maxTip, common.BigToHash(power))
			b = newNTipHash(maxTip, common.BigToHash(below))
			require.Equal(t, 1, a.Cmp(b))
			require.Equal(t, -1, b.Cmp(a))
		})
	}
}
