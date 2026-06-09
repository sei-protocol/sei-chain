//go:build mock_chain_validation

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsensusPolicy_MockChainValidation_SwallowMatrix(t *testing.T) {
	policy := DefaultConsensusPolicy()
	for _, kind := range ValidationErrorKinds() {
		// A contextual error wrapping the sentinel must match it under errors.Is.
		err := fmt.Errorf("sentinel %s: %w", kind.Kind, kind)
		got := policy.HandleError(err)
		if kind == ErrLastCommitVerify {
			if got != err {
				t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(%q) = %v, want the input error (kind is excluded from swallow set)", kind.Kind, got)
			}
			continue
		}
		if got != nil {
			t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(%q) = %v, want nil", kind.Kind, got)
		}
	}
}

// A swallowed error built with multi-%w (the production shape at the
// replay/validation/evidence-overflow call sites) must still expose the OUTER
// sentinel's Kind for the metric — recordUnsafeValidationSkipped does
// errors.As(err, &*ConsensusPolicyError).Kind, which must resolve to the
// sentinel, not the inner cause — while keeping the inner cause reachable.
func TestConsensusPolicy_MockChainValidation_WrappedCausePreservesLabel(t *testing.T) {
	policy := DefaultConsensusPolicy()
	inner := errors.New("inner cause")
	err := fmt.Errorf("%w: %w", ErrEvidenceOverflowKind, inner)

	if got := policy.HandleError(err); got != nil {
		t.Fatalf("HandleError(wrapped) = %v, want nil (swallowed)", got)
	}

	var cpe *ConsensusPolicyError
	if !errors.As(err, &cpe) {
		t.Fatal("errors.As(err, *ConsensusPolicyError) = false, want true")
	}
	if cpe.Kind != "evidence_overflow" {
		t.Errorf("metric label = %q, want evidence_overflow (outer sentinel, not inner cause)", cpe.Kind)
	}
	if !errors.Is(err, inner) {
		t.Error("errors.Is(err, inner) = false, want true (the %w cause must stay reachable)")
	}
}

func TestConsensusPolicy_MockChainValidation_UnknownErrorReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(testErr); got != testErr {
		t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(unknown) = %v, want testErr", got)
	}
}
