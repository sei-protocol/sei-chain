//go:build !mock_block_validation && !mock_chain_validation

package types

import (
	"errors"
	"testing"
)

func TestConsensusPolicy_Default_AllKindsReturnErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	for _, kind := range ValidationErrorKinds() {
		err := kind.With("sentinel %s", kind.Label())
		if got := policy.HandleError(err); got != err {
			t.Errorf("default ConsensusPolicy.HandleError(%q) = %v, want the input error", kind.Label(), got)
		}
	}
}

func TestConsensusPolicy_Default_UnknownErrorReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(testErr); got != testErr {
		t.Errorf("default ConsensusPolicy.HandleError(unknown) = %v, want testErr", got)
	}
}

func TestValidationErrorKinds_Count(t *testing.T) {
	got := len(ValidationErrorKinds())
	if got != 13 {
		t.Errorf("ValidationErrorKinds() returned %d kinds, want 13 (per M1.0 audit)", got)
	}
}
