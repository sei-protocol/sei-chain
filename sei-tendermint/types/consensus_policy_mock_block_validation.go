//go:build mock_block_validation

package types

import "errors"

// Swallow set is ErrAppHash + ErrDataHash (the two block-validation checks this
// tag has always relaxed) plus ErrUpgradeBeforeTrigger, which lets a replay run a
// binary that already contains upgrade handlers for heights it has not yet
// reached. All other audit-row failures halt as in production.
type ConsensusPolicy struct{}

func (ConsensusPolicy) HandleError(err error) error {
	if errors.Is(err, ErrAppHash) || errors.Is(err, ErrDataHash) || errors.Is(err, ErrUpgradeBeforeTrigger) {
		recordUnsafeValidationSkipped(err)
		return nil
	}
	return err
}
