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

func GetMiddleManAddress(ctx sdk.Context) sdk.AccAddress {
	txIndexBz := make([]byte, 8)
	binary.BigEndian.PutUint64(txIndexBz, uint64(ctx.TxIndex()))
	return sdk.AccAddress(append(MiddleManAddressPrefix, txIndexBz...))
}

func GetCoinbaseAddress(ctx sdk.Context) sdk.AccAddress {
	txIndexBz := make([]byte, 8)
	binary.BigEndian.PutUint64(txIndexBz, uint64(ctx.TxIndex()))
	return sdk.AccAddress(append(CoinbaseAddressPrefix, txIndexBz...))
}
