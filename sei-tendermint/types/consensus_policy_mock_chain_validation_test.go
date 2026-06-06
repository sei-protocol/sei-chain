//go:build mock_chain_validation

package types

import (
	"errors"
	"testing"
)

func TestConsensusPolicy_MockChainValidation_AllKindsSwallowed(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	for _, kind := range ValidationErrorKinds() {
		if got := policy.HandleError(kind, testErr); got != nil {
			t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(%q, testErr) = %v, want nil", kind, got)
		}
	}
}

func TestConsensusPolicy_MockChainValidation_UnknownKindReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(ErrorKind("not_a_real_kind"), testErr); got != testErr {
		t.Errorf("mock_chain_validation ConsensusPolicy.HandleError(unknown, testErr) = %v, want testErr", got)
	}
}
