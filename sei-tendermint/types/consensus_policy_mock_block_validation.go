//go:build mock_block_validation

package types

import "errors"

// Swallow set is ErrAppHash + ErrDataHash only — these are the two checks the
// mock_block_validation tag has always relaxed; preserving that exact set
// keeps user-visible outcomes under this tag unchanged across the refactor.
// All other audit-row kinds halt as in production.
type ConsensusPolicy struct{}

func (ConsensusPolicy) HandleError(err error) error {
	if errors.Is(err, ErrAppHash) || errors.Is(err, ErrDataHash) {
		recordUnsafeValidationSkipped(err)
		return nil
	}
	return err
}
