package utils

import (
	"encoding/binary"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/goutils"
)

func DecToBigEndian(d sdk.Dec) (res []byte) {
	i := d.BigInt()
	words := i.Bits()
	// words are little-endian but we want big-endian so we start from the back
	for idx := len(words) - 1; idx >= 0; idx-- {
		bz := make([]byte, 8)
		word := uint64(words[idx])
		if d.IsNegative() {
			word = ^word
		}
		binary.BigEndian.PutUint64(bz, word)
		goutils.InPlaceAppend(&res, bz...)
	}
	lastZeroByteIdx := -1
	for i := 0; i < len(res); i++ {
		if res[i] != 0 {
			break
		}
		lastZeroByteIdx = i
	}
	numNonZeroBytes := uint32(len(res) - lastZeroByteIdx - 1)
	if d.IsNegative() {
		numNonZeroBytes = ^numNonZeroBytes
	}
	lengthHeaderBz := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthHeaderBz, numNonZeroBytes)
	res = goutils.ImmutableAppend(lengthHeaderBz, res[lastZeroByteIdx+1:]...)
	if d.IsNegative() {
		res = goutils.ImmutableAppend([]byte{0}, res...)
	} else {
		res = goutils.ImmutableAppend([]byte{1}, res...)
	}
	return res
}

func BytesToDec(bz []byte) sdk.Dec {
	neg := bz[0] == 0
	length := binary.BigEndian.Uint32(bz[1:5])
	if neg {
		length = ^length
	}
	paddingLength := 0
	if length%8 != 0 {
		paddingLength = 8 - int(length)%8
	}
	padding := make([]byte, paddingLength)
	bz = goutils.ImmutableAppend(padding, bz[5:]...)
	words := []big.Word{}
	for i := 0; i < len(bz); i += 8 {
		word := binary.BigEndian.Uint64(bz[i : i+8])
		if neg {
			word = ^word
		}
		words = goutils.ImmutableAppend([]big.Word{big.Word(word)}, words...)
	}
	bi := &big.Int{}
	bi.SetBits(words)
	if neg {
		bi.Neg(bi)
	}
	return sdk.NewDecFromBigIntWithPrec(bi, sdk.Precision)
}
