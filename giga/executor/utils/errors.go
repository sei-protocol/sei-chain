package utils

import (
	"errors"

	"github.com/sei-protocol/sei-chain/giga/executor/precompiles"
)

func ShouldExecutionAbort(err error) bool {
	return errors.Is(err, precompiles.ErrInvalidPrecompileCall)
}
