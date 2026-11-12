package staking

import (
	"math/big"
)

var (
	// weiPerUsei is the wei in one usei, the smallest unit an amount may hold.
	weiPerUsei = big.NewInt(1_000_000_000_000)

	// powerReduction is the tokens in one unit of consensus power: one SEI, as
	// x/staking's DefaultPowerReduction of 1e6 usei is.
	powerReduction = new(big.Int).Mul(big.NewInt(1_000_000), weiPerUsei)
)

// consensusPower returns the consensus power tokens are worth.
func consensusPower(tokens *big.Int) *big.Int {
	return new(big.Int).Quo(tokens, powerReduction)
}
