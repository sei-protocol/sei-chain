package state

import "math/big"

// UseiToSweiMultiplier Fields that were denominated in usei will be converted to swei (1usei = 10^12swei)
// for existing Ethereum application (which assumes 18 decimal points) to display properly.
var UseiToSweiMultiplier = big.NewInt(1_000_000_000_000)
