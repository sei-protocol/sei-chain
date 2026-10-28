package params

import (
	"errors"
	"math/big"
)

var (
	decOne = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	errInvalidValue = errors.New("invalid param value")
)
