package utils

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

var DECS = []sdk.Dec{
	sdk.MustNewDecFromStr("90.0"),
	sdk.MustNewDecFromStr("10.01"),
	sdk.MustNewDecFromStr("10"),
	sdk.MustNewDecFromStr("9.99"),
	sdk.MustNewDecFromStr("9.9"),
	sdk.MustNewDecFromStr("9.0"),
	sdk.MustNewDecFromStr("1"),
	sdk.MustNewDecFromStr("0.001"),
	sdk.MustNewDecFromStr("0"),
	sdk.MustNewDecFromStr("-0.001"),
	sdk.MustNewDecFromStr("-1"),
	sdk.MustNewDecFromStr("-9.0"),
	sdk.MustNewDecFromStr("-9.9"),
	sdk.MustNewDecFromStr("-9.99"),
	sdk.MustNewDecFromStr("-10"),
	sdk.MustNewDecFromStr("-10.01"),
	sdk.MustNewDecFromStr("-90.0"),
}

func TestDecToBigEndian(t *testing.T) {
	for i := 1; i < len(DECS); i++ {
		require.True(t, compBz(DecToBigEndian(DECS[i-1]), DecToBigEndian(DECS[i])))
	}
}

func TestBytesToDec(t *testing.T) {
	for _, dec := range DECS {
		require.Equal(t, dec, BytesToDec(DecToBigEndian(dec)))
	}
}

func compBz(b1 []byte, b2 []byte) bool {
	ptr := 0
	for ; ptr < len(b1) && ptr < len(b2); ptr++ {
		if b1[ptr] < b2[ptr] {
			return false
		}
		if b1[ptr] > b2[ptr] {
			return true
		}
	}

	return ptr >= len(b2)
}
