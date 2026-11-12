// Package dec is arithmetic on decimals held as big.Int values scaled by 1e18,
// rounding as the Cosmos SDK's sdk.Dec does.
package dec

import (
	"math/big"
)

// one is 1 as a decimal.
var one = big.NewInt(1_000_000_000_000_000_000)

// One returns 1 as a decimal.
func One() *big.Int {
	return new(big.Int).Set(one)
}

// FromInt returns integer i as a decimal.
func FromInt(i *big.Int) *big.Int {
	return new(big.Int).Mul(i, one)
}

// Truncate returns non-negative decimal d with its fraction dropped.
func Truncate(d *big.Int) *big.Int {
	return new(big.Int).Quo(d, one)
}

// Quo divides decimal a by decimal b, both non-negative, as sdk.Dec's Quo
// does: truncated to 36 places, then rounded half to even to 18.
func Quo(a, b *big.Int) *big.Int {
	q := new(big.Int).Mul(a, one)
	q.Mul(q, one)
	return round(q.Quo(q, b))
}

// round divides non-negative x by 1e18, rounding half to even, as sdk.Dec's
// chopPrecisionAndRound does.
func round(x *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(x, one, new(big.Int))
	switch r.Lsh(r, 1).Cmp(one) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}
