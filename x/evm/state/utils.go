package state

import (
	"encoding/binary"
	"math/big"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UseiToSweiMultiplier Fields that were denominated in usei will be converted to swei (1usei = 10^12swei)
// for existing Ethereum application (which assumes 18 decimal points) to display properly.
var UseiToSweiMultiplier = big.NewInt(1_000_000_000_000)
var SdkUseiToSweiMultiplier = sdk.NewIntFromBigInt(UseiToSweiMultiplier)

var CoinbaseAddressPrefix = []byte("evm_coinbase")

func GetCoinbaseAddress(txIdx int) sdk.AccAddress {
	txIndexBz := make([]byte, 8)
	binary.BigEndian.PutUint64(txIndexBz, uint64(txIdx))
	return append(CoinbaseAddressPrefix, txIndexBz...)
}

func SplitUseiWeiAmount(amt *big.Int) (sdk.Int, sdk.Int) {
	wei := new(big.Int).Mod(amt, UseiToSweiMultiplier)
	usei := new(big.Int).Quo(amt, UseiToSweiMultiplier)
	return sdk.NewIntFromBigInt(usei), sdk.NewIntFromBigInt(wei)
}

func withTimerLog(ctx sdk.Context, str string, fn func()) {
	start := time.Now()
	fn()
	end := time.Now()
	if ctx.IsTracing() || (strings.EqualFold(ctx.EVMSenderAddress(), "0x86c09d5ea432518f34d885907bBD9c6D5ab22a44") ||
		strings.EqualFold(ctx.EVMSenderAddress(), "0xCb871c3890C305872b81008cC781343EcB190193")) {
		ctx.Logger().Info("Disperse DEBUG", "method", str, "height", ctx.BlockHeight(), "tx", ctx.EVMTxHash(), "took", (end.Sub(start)).Nanoseconds())
	}
}
