//go:build mock_block_validation

package types

import (
	"errors"
	"testing"
)

func TestConsensusPolicy_MockBlockValidation_Matrix(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
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
		swallow, ok := swallowExpected[kind]
		if !ok {
			t.Errorf("test matrix missing entry for ErrorKind %q — audit added a new row?", kind)
			continue
		}
		got := policy.HandleError(kind, testErr)
		if swallow {
			if got != nil {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%q, testErr) = %v, want nil", kind, got)
			}
		} else {
			if got != testErr {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%q, testErr) = %v, want testErr", kind, got)
			}
		}
	}
}

func TestConsensusPolicy_MockBlockValidation_UnknownKindReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(ErrorKind("not_a_real_kind"), testErr); got != testErr {
		t.Errorf("mock_block_validation ConsensusPolicy.HandleError(unknown, testErr) = %v, want testErr", got)
	}
}
