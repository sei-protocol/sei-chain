package evm

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestNewNTipHashEncoding(t *testing.T) {
	tip := uint256.Int{1, 2, 3, 4}
	hash := common.Hash{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
		24, 25, 26, 27, 28, 29, 30, 31,
	}
	before := tip
	require.Equal(t, nTipHash{
		^uint64(4), ^uint64(3), ^uint64(2), ^uint64(1),
		0x0001020304050607, 0x08090a0b0c0d0e0f,
		0x1011121314151617, 0x18191a1b1c1d1e1f,
	}, newNTipHash(tip, hash))
	require.Equal(t, before, tip)
}

func TestNTipHashOrderingBoundaries(t *testing.T) {
	tips := []uint256.Int{
		{}, {1}, {math.MaxUint64}, {0, 1},
		{0, 0, 1}, {0, 0, 0, 1},
		{math.MaxUint64 - 1, math.MaxUint64, math.MaxUint64, math.MaxUint64},
		{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64},
	}
	for _, a := range tips {
		for _, b := range tips {
			// Opposite hash order proves tip priority dominates all hash words.
			for _, hashes := range [][2]common.Hash{
				{{}, {}},
				{{1}, {}},
				{{}, {1}},
			} {
				want := -a.Cmp(&b)
				if want == 0 {
					want = hashes[0].Cmp(hashes[1])
				}
				require.Equal(t, want, newNTipHash(a, hashes[0]).Cmp(newNTipHash(b, hashes[1])))
			}
		}
	}
}

func TestNTipHashOrderingMatchesBigInt(t *testing.T) {
	rng := utils.TestRng()
	for prefix := range 9 {
		for range 100 {
			var a, b nTipHash
			for i := range a {
				a[i], b[i] = rng.Uint64(), rng.Uint64()
			}
			copy(b[:prefix], a[:prefix])
			aTip := uint256.Int{a[0], a[1], a[2], a[3]}
			bTip := uint256.Int{b[0], b[1], b[2], b[3]}
			var aHash, bHash common.Hash
			for i := range 4 {
				binary.BigEndian.PutUint64(aHash[8*i:], a[i+4])
				binary.BigEndian.PutUint64(bHash[8*i:], b[i+4])
			}
			want := -aTip.ToBig().Cmp(bTip.ToBig())
			if want == 0 {
				want = aHash.Cmp(bHash)
			}
			aKey, bKey := newNTipHash(aTip, aHash), newNTipHash(bTip, bHash)
			require.Equal(t, want, aKey.Cmp(bKey))
			require.Equal(t, -want, bKey.Cmp(aKey))
		}
	}
}

func TestNTipHashCmpEveryWord(t *testing.T) {
	for firstDifference := range 8 {
		for _, values := range [][2]uint64{
			{0, 1}, {0, math.MaxUint64}, {math.MaxUint64 - 1, math.MaxUint64},
		} {
			var a, b nTipHash
			a[firstDifference], b[firstDifference] = values[0], values[1]
			// Opposite suffix ordering must not outweigh the first differing word.
			for i := firstDifference + 1; i < len(a); i++ {
				a[i] = math.MaxUint64
			}
			require.Equal(t, -1, a.Cmp(b))
			require.Equal(t, 1, b.Cmp(a))
			require.Equal(t, 0, a.Cmp(a))
		}
	}
}
