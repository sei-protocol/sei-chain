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

var swallowedKinds = func() []*ConsensusPolicyError {
	kinds := make([]*ConsensusPolicyError, 0, len(ValidationErrors()))
	for _, k := range ValidationErrors() {
		// Excluded — would panic downstream in buildLastCommitInfo.
		if k == ErrLastCommitVerify {
			continue
		}
		kinds = append(kinds, k)
	}
	return kinds
}()

func (ConsensusPolicy) HandleError(err error) error {
	for _, k := range swallowedKinds {
		if errors.Is(err, k) {
			recordUnsafeValidationSkipped(err)
			return nil
		}
	}
	return err
}
