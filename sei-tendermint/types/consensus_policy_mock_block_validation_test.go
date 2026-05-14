//go:build mock_block_validation

package types

import "testing"

func TestConsensusPolicy_MockBlockValidation_Matrix(t *testing.T) {
	policy := DefaultConsensusPolicy()
	swallowExpected := map[ErrorKind]bool{
		ErrorKindAppHash:                   true,
		ErrorKindDataHash:                  true,
		ErrorKindLastResultsHash:           false,
		ErrorKindLastBlockID:               false,
		ErrorKindConsensusHash:             false,
		ErrorKindValidatorsHash:            false,
		ErrorKindNextValidatorsHash:        false,
		ErrorKindLastCommitVerify:          false,
		ErrorKindProposerNotInValidatorSet: false,
		ErrorKindEvidenceOverflow:          false,
		ErrorKindLastCommitHash:            false,
		ErrorKindEvidenceHash:              false,
		ErrorKindPerEvidenceValidateBasic:  false,
	}
	for _, kind := range AllSwallowEligibleErrorKinds() {
		want, ok := swallowExpected[kind]
		if !ok {
			t.Errorf("test matrix missing entry for ErrorKind %q — audit added a new row?", kind)
			continue
		}
		if got := policy.ShouldSwallow(kind); got != want {
			t.Errorf("mock_block_validation ConsensusPolicy.ShouldSwallow(%q) = %v, want %v",
				kind, got, want)
		}
	}
}

func TestConsensusPolicy_MockBlockValidation_UnknownKindHalts(t *testing.T) {
	policy := DefaultConsensusPolicy()
	if policy.ShouldSwallow(ErrorKind("not_a_real_kind")) {
		t.Errorf("mock_block_validation ConsensusPolicy.ShouldSwallow(unknown) = true, want false")
	}
}
