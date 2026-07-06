//go:build mock_block_validation

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsensusPolicy_MockBlockValidation_Matrix(t *testing.T) {
	policy := DefaultConsensusPolicy()
	swallowExpected := map[error]bool{
		ErrAppHash:                   true,
		ErrDataHash:                  true,
		ErrLastResultsHash:           false,
		ErrLastBlockID:               false,
		ErrConsensusHash:             false,
		ErrValidatorsHash:            false,
		ErrNextValidatorsHash:        false,
		ErrLastCommitVerify:          false,
		ErrProposerNotInValidatorSet: false,
		ErrTooMuchEvidence:           false,
		ErrLastCommitHash:            false,
		ErrEvidenceHash:              false,
		ErrPerEvidenceValidateBasic:  false,
		ErrUpgradeBeforeTrigger:      true,
	}
	for _, sentinel := range ValidationErrors() {
		swallow, ok := swallowExpected[sentinel]
		if !ok {
			t.Errorf("test matrix missing entry for %v — audit added a new sentinel?", sentinel)
			continue
		}
		// A contextual error wrapping the sentinel must match it under errors.Is.
		err := fmt.Errorf("validation failed: %w", sentinel)
		got := policy.HandleError(err)
		if swallow {
			if got != nil {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%v) = %v, want nil", sentinel, got)
			}
		} else {
			if got != err {
				t.Errorf("mock_block_validation ConsensusPolicy.HandleError(%v) = %v, want the input error", sentinel, got)
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
