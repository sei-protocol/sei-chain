package utils

import (
	"errors"

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

// ExecutionFailedAbortError signals a state-transition error from geth's
// Execute path. V2 owns the canonical fee, nonce, receipt, and ABCI result
// handling for these failures, so the caller should fall back to v2.
type ExecutionFailedAbortError struct{}

func (e *ExecutionFailedAbortError) Error() string {
	return "EVM execution failed; v2 produces the canonical failure result"
}

func (e *ExecutionFailedAbortError) IsAbortError() bool {
	return true
}

var ErrExecutionFailed error = &ExecutionFailedAbortError{}

// ShouldExecutionAbort reports whether err, or any error it wraps, is an AbortError that
// should cause Giga execution to abort and fall back to standard execution.
func ShouldExecutionAbort(err error) bool {
	var abortErr vm.AbortError
	return errors.As(err, &abortErr) && abortErr.IsAbortError()
}
