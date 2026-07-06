//go:build mock_chain_validation

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsensusPolicy_MockChainValidation_SwallowMatrix(t *testing.T) {
	policy := DefaultConsensusPolicy()

	swallowed := make(map[error]bool, len(swallowedErrors))
	for _, e := range swallowedErrors {
		swallowed[e] = true
	}

	// Semantic guard: peer-supplied block-content integrity must NEVER be in the
	// swallow allowlist, so a malformed/lying peer cannot silently poison the
	// audit input. This is the load-bearing safety property of this build.
	for _, e := range []error{ErrDataHash, ErrEvidenceHash, ErrPerEvidenceValidateBasic} {
		if swallowed[e] {
			t.Errorf("%v is in the swallow allowlist; peer-supplied content integrity must halt", e)
		}
	}

	// HandleError swallows exactly the allowlist; everything else in the audit set
	// halts, including any newly added sentinel (the allowlist is halt-by-default).
	for _, sentinel := range ValidationErrors() {
		got := policy.HandleError(fmt.Errorf("validation failed: %w", sentinel))
		switch {
		case swallowed[sentinel] && got != nil:
			t.Errorf("HandleError(%v) = %v, want swallowed (nil)", sentinel, got)
		case !swallowed[sentinel] && got == nil:
			t.Errorf("HandleError(%v) = nil, want HALT (not in the swallow allowlist)", sentinel)
		}
	}
}

// A multi-%w wrapped error must resolve to the OUTER sentinel's label while
// keeping the inner cause reachable via errors.Is.
func TestConsensusPolicy_MockChainValidation_WrappedCausePreservesLabel(t *testing.T) {
	policy := DefaultConsensusPolicy()
	inner := errors.New("inner cause")
	err := fmt.Errorf("%w: %w", ErrTooMuchEvidence, inner)

	if got := policy.HandleError(err); got != nil {
		t.Fatalf("HandleError(wrapped) = %v, want nil (swallowed)", got)
	}
	if got := validationLabel(err); got != "too_much_evidence" {
		t.Errorf("validationLabel(wrapped) = %q, want too_much_evidence (outer sentinel, not inner cause)", got)
	}
	if !errors.Is(err, inner) {
		t.Error("errors.Is(err, inner) = false, want true (the %w cause must stay reachable)")
	}
}

// Every sentinel must map to a label; a missing validationLabel case would
// silently emit validation_error="unknown".
func TestValidationLabels_AllSentinelsMapped(t *testing.T) {
	for _, sentinel := range ValidationErrors() {
		if got := validationLabel(sentinel); got == "unknown" {
			t.Errorf("validationLabel(%v) = unknown; sentinel is missing a case in policy_metrics.go", sentinel)
		}
	}
}

func TestConsensusPolicy_MockChainValidation_UnknownErrorReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(testErr); got != testErr {
		t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(unknown) = %v, want testErr", got)
	}
}
