//go:build mock_block_validation

package types

import (
	"errors"
	"fmt"
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
		ErrEvidenceOverflowCode:      false,
		ErrLastCommitHash:            false,
		ErrEvidenceHash:              false,
		ErrPerEvidenceValidateBasic:  false,
	}
	for _, kind := range ValidationErrors() {
		swallow, ok := swallowExpected[kind]
		if !ok {
			t.Errorf("test matrix missing entry for kind %q — audit added a new row?", kind.Code)
			continue
		}
		// A contextual error wrapping the sentinel must match it under errors.Is.
		err := fmt.Errorf("sentinel %s: %w", kind.Code, kind)
		got := policy.HandleError(err)
		if swallow {
			if got != nil {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%q) = %v, want nil", kind.Code, got)
			}
		} else {
			if got != err {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%q) = %v, want the input error", kind.Code, got)
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
