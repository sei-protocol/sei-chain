//go:build mock_block_validation

package types

import (
	"errors"
	"testing"
)

func TestConsensusPolicy_MockBlockValidation_Matrix(t *testing.T) {
	policy := DefaultConsensusPolicy()
	swallowExpected := map[*ConsensusPolicyError]bool{
		ErrAppHash:                   true,
		ErrDataHash:                  true,
		ErrLastResultsHash:           false,
		ErrLastBlockID:               false,
		ErrConsensusHash:             false,
		ErrValidatorsHash:            false,
		ErrNextValidatorsHash:        false,
		ErrLastCommitVerify:          false,
		ErrProposerNotInValidatorSet: false,
		ErrEvidenceOverflowKind:      false,
		ErrLastCommitHash:            false,
		ErrEvidenceHash:              false,
		ErrPerEvidenceValidateBasic:  false,
	}
	for _, kind := range ValidationErrorKinds() {
		swallow, ok := swallowExpected[kind]
		if !ok {
			t.Errorf("test matrix missing entry for kind %q — audit added a new row?", kind.Label())
			continue
		}
		// A contextual error built via With must match its sentinel under errors.Is.
		err := kind.With("sentinel %s", kind.Label())
		got := policy.HandleError(err)
		if swallow {
			if got != nil {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%q) = %v, want nil", kind.Label(), got)
			}
		} else {
			if got != err {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%q) = %v, want the input error", kind.Label(), got)
			}
		}
	}
}

func TestConsensusPolicy_MockBlockValidation_UnknownErrorReturnsErr(t *testing.T) {
	policy := DefaultConsensusPolicy()
	testErr := errors.New("sentinel")
	if got := policy.HandleError(testErr); got != testErr {
		t.Errorf("mock_block_validation ConsensusPolicy.HandleError(unknown) = %v, want testErr", got)
	}
}
