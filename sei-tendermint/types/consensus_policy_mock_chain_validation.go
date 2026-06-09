//go:build mock_chain_validation

package types

import "errors"

// ConsensusPolicy in mock_chain_validation builds swallows every
// swallow-eligible sentinel enumerated by ValidationErrors except
// ErrLastCommitVerify (excluded to avoid a downstream panic in
// buildLastCommitInfo) — the chain computes every check authentically and
// logs nothing here. A swallowed failure increments the counter and does
// not halt; ErrLastCommitVerify is the exception — it halts and is not
// counted.
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
