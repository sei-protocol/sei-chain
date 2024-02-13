package state

import (
	"encoding/binary"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UseiToSweiMultiplier Fields that were denominated in usei will be converted to swei (1usei = 10^12swei)
// for existing Ethereum application (which assumes 18 decimal points) to display properly.
var UseiToSweiMultiplier = big.NewInt(1_000_000_000_000)

var MiddleManAddressPrefix = []byte("evm_middleman")
var CoinbaseAddressPrefix = []byte("evm_coinbase")
var WeiTmpEscrowPrefix = []byte("evm_weiescrow")

func GetMiddleManAddress(txIdx int) sdk.AccAddress {
	txIndexBz := make([]byte, 8)
	binary.BigEndian.PutUint64(txIndexBz, uint64(txIdx))
	return sdk.AccAddress(append(MiddleManAddressPrefix, txIndexBz...))
}

func GetCoinbaseAddress(txIdx int) sdk.AccAddress {
	txIndexBz := make([]byte, 8)
	binary.BigEndian.PutUint64(txIndexBz, uint64(txIdx))
	return sdk.AccAddress(append(CoinbaseAddressPrefix, txIndexBz...))
}

func GetTempWeiEscrowAddress(txIdx int) sdk.AccAddress {
	txIndexBz := make([]byte, 8)
	binary.BigEndian.PutUint64(txIndexBz, uint64(txIdx))
	return sdk.AccAddress(append(WeiTmpEscrowPrefix, txIndexBz...))
}

func SplitUseiWeiAmount(amt *big.Int) (usei *big.Int, wei *big.Int) {
	wei = new(big.Int).Mod(amt, UseiToSweiMultiplier)
	usei = new(big.Int).Quo(amt, UseiToSweiMultiplier)
	return
}
