package staking

import (
	"math/big"
)

var (
	// decOne is 1 as a 1e18-scaled decimal, the scale of shares and ratios.
	decOne = big.NewInt(1_000_000_000_000_000_000)

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

// intToDec returns integer i as a decimal.
func intToDec(i *big.Int) *big.Int {
	return new(big.Int).Mul(i, decOne)
}

// truncateDec returns non-negative decimal d with its fraction dropped.
func truncateDec(d *big.Int) *big.Int {
	return new(big.Int).Quo(d, decOne)
}

// quoDec divides decimal a by decimal b, both non-negative, as sdk.Dec's Quo
// does: truncated to 36 places, then rounded half to even to 18.
func quoDec(a, b *big.Int) *big.Int {
	q := new(big.Int).Mul(a, decOne)
	q.Mul(q, decOne)
	return roundDec(q.Quo(q, b))
}

// roundDec divides non-negative x by 1e18, rounding half to even, as
// sdk.Dec's chopPrecisionAndRound does.
func roundDec(x *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(x, decOne, new(big.Int))
	switch r.Lsh(r, 1).Cmp(decOne) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}
