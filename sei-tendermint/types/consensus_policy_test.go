//go:build !mock_block_validation && !mock_chain_validation

package types

import "testing"

func TestConsensusPolicy_Default_AllKindsHalt(t *testing.T) {
	policy := DefaultConsensusPolicy()
	for _, kind := range AllSwallowEligibleErrorKinds() {
		if policy.ShouldSwallow(kind) {
			t.Errorf("default ConsensusPolicy.ShouldSwallow(%q) = true, want false", kind)
		}
	}
}

func TestConsensusPolicy_Default_UnknownKindHalts(t *testing.T) {
	policy := DefaultConsensusPolicy()
	if policy.ShouldSwallow(ErrorKind("not_a_real_kind")) {
		t.Errorf("default ConsensusPolicy.ShouldSwallow(unknown) = true, want false")
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
