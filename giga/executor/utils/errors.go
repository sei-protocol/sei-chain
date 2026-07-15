package utils

import (
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	// GigaAbortCodespace and GigaAbortCode identify a sentinel ResponseDeliverTx
	// that signals the caller to fall back to the v2 execution path. The
	// response's Info field carries the abort cause for the fallback metric.
	GigaAbortCodespace = "giga"
	GigaAbortCode      = uint32(1)
)

// ValidationFailedAbortError signals an EVM validation failure (fee/nonce/
// balance), whose canonical failure receipt only v2's ante chain can produce;
// the caller should fall back to v2.
type ValidationFailedAbortError struct{}

func (e *ValidationFailedAbortError) Error() string {
	return "EVM validation failed; v2 produces the canonical failure receipt"
}

func (e *ValidationFailedAbortError) IsAbortError() bool {
	return true
}

var ErrValidationFailed error = &ValidationFailedAbortError{}

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
