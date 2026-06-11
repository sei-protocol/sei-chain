//go:build mock_chain_validation

package types

import "errors"

// ConsensusPolicy here swallows every ValidationErrors sentinel except
// ErrLastCommitVerify (excluded — it would panic downstream in
// buildLastCommitInfo). A swallowed failure increments the counter and
// continues; ErrLastCommitVerify halts and is not counted.
type ConsensusPolicy struct{}

var swallowedErrors = func() []error {
	errs := make([]error, 0, len(ValidationErrors()))
	for _, e := range ValidationErrors() {
		// Excluded — would panic downstream in buildLastCommitInfo.
		if e == ErrLastCommitVerify {
			continue
		}
		errs = append(errs, e)
	}
	return errs
}()

func (ConsensusPolicy) HandleError(err error) error {
	for _, e := range swallowedErrors {
		if errors.Is(err, e) {
			recordUnsafeValidationSkipped(err)
			return nil
		}
	}
	return err
}
