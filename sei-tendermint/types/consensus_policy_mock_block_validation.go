//go:build mock_block_validation

package types

// ConsensusPolicy in mock_block_validation builds: swallows AppHash and
// DataHash mismatches. The two checks compute their comparison
// authentically (no more pre-comparison short-circuit), and the divergence
// is logged before continuing. All other audit-row checks halt as in
// production.
//
// Behavior change relative to the prior Skip*Validation shape:
//   - The Data.Hash(false) compute fast-path is gone (explicitly accepted).
//   - AppHash / DataHash mismatches are now LOGGED in addition to being
//     bypassed (previously they were silently skipped).
//
// User-visible outcome is preserved: the chain still progresses past
// AppHash and DataHash mismatches under this tag.
type ConsensusPolicy struct{}

// ShouldSwallow returns true for the two CometBFT hash checks that the
// mock_block_validation tag has always relaxed. All other ErrorKinds halt.
func (ConsensusPolicy) ShouldSwallow(kind ErrorKind) bool {
	switch kind {
	case ErrorKindAppHash, ErrorKindDataHash:
		return true
	default:
		return false
	}
}
