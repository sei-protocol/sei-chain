//go:build !mock_block_validation && !mock_chain_validation

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsensusPolicy_Default_AllReturnErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	for _, sentinel := range ValidationErrors() {
		err := fmt.Errorf("validation failed: %w", sentinel)
		if got := policy.HandleError(err); got != err {
			t.Errorf("default ConsensusPolicy.HandleError(%v) = %v, want the input error", sentinel, got)
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

func TestValidationErrors_Count(t *testing.T) {
	got := len(ValidationErrors())
	if got != 14 {
		t.Errorf("ValidationErrors() returned %d sentinels, want 14", got)
	}
}

// A wrapped sentinel must be matchable two ways: errors.Is against a specific
// sentinel (which one), and errors.As against the type (the whole class).
func TestConsensusPolicyError_IsAndAs(t *testing.T) {
	err := fmt.Errorf("wrong Block.Header.AppHash: %w", ErrAppHash)

	if !errors.Is(err, ErrAppHash) {
		t.Error("errors.Is(err, ErrAppHash) = false, want true (specific sentinel)")
	}
	if errors.Is(err, ErrDataHash) {
		t.Error("errors.Is(err, ErrDataHash) = true, want false (distinct sentinel)")
	}

	var cpe *ConsensusPolicyError
	if !errors.As(err, &cpe) {
		t.Fatal("errors.As(err, *ConsensusPolicyError) = false, want true (class recovery)")
	}
	if cpe != ErrAppHash {
		t.Errorf("errors.As recovered %v, want the ErrAppHash sentinel", cpe)
	}
}
