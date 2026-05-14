//go:build mock_block_validation

package types

// Swallow set is AppHash + DataHash only — these are the two checks the
// mock_block_validation tag has always relaxed; preserving that exact set
// keeps user-visible outcomes under this tag unchanged across the refactor.
// All other audit-row kinds halt as in production.
type ConsensusPolicy struct{}

func (ConsensusPolicy) ShouldSwallow(kind ErrorKind, err error) error {
	switch kind {
	case ErrorKindAppHash, ErrorKindDataHash:
		unsafeValidationSkippedTotal.WithLabelValues(string(kind)).Inc()
		return nil
	default:
		return err
	}
}
