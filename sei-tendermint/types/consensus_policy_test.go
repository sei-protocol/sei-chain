//go:build !mock_block_validation && !mock_chain_validation

package types

import (
	"errors"
	"testing"
)

func TestConsensusPolicy_Default_AllKindsReturnErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	for _, kind := range AllSwallowEligibleErrorKinds() {
		if got := policy.HandleError(kind, testErr); got != testErr {
			t.Errorf("default ConsensusPolicy.HandleError(%q, testErr) = %v, want testErr", kind, got)
		}
	}
}

func TestConsensusPolicy_Default_UnknownKindReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(ErrorKind("not_a_real_kind"), testErr); got != testErr {
		t.Errorf("default ConsensusPolicy.HandleError(unknown, testErr) = %v, want testErr", got)
	}
}

// Guards the M1.0 audit's 13-row invariant — a change here means the audit
// (docs/designs/mock-chain-validation-m1-audit.md) needs to be revisited.
func TestAllSwallowEligibleErrorKinds_Count(t *testing.T) {
	got := len(AllSwallowEligibleErrorKinds())
	if got != 13 {
		t.Errorf("AllSwallowEligibleErrorKinds() returned %d kinds, want 13 (per M1.0 audit)", got)
	}
}
