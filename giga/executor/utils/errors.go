package utils

import (
	"github.com/ethereum/go-ethereum/core/vm"
)

// ShouldExecutionAbort checks if the given error is an AbortError that should
// cause Giga execution to abort and fall back to standard execution.
func ShouldExecutionAbort(err error) bool {
	if err == nil {
		return false
	}
	if abortErr, ok := err.(vm.AbortError); ok {
		return abortErr.IsAbortError()
	}
	return false
}
