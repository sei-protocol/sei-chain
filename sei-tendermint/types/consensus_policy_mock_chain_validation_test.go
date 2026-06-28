//go:build mock_chain_validation

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsensusPolicy_MockChainValidation_SwallowMatrix(t *testing.T) {
	policy := DefaultConsensusPolicy()
	for _, sentinel := range ValidationErrors() {
		// A contextual error wrapping the sentinel must match it under errors.Is.
		err := fmt.Errorf("validation failed: %w", sentinel)
		if got := policy.HandleError(err); got != nil {
			t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(%v) = %v, want nil (every sentinel is swallowed, incl. ErrLastCommitVerify)", sentinel, got)
		}
	}
}

// mock_chain_validation swallows ErrLastCommitVerify, so buildLastCommitInfo must
// be told to tolerate a commit/validator-set size divergence rather than panic.
func TestConsensusPolicy_MockChainValidation_TolerateLastCommitMismatch(t *testing.T) {
	if !DefaultConsensusPolicy().TolerateLastCommitMismatch() {
		t.Error("mock_chain_validation ConsensusPolicy.TolerateLastCommitMismatch() = false, want true")
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
