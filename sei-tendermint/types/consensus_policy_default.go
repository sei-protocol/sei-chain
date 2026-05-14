//go:build !mock_block_validation && !mock_chain_validation

package types

// ConsensusPolicy in production builds: never swallows. Every call site
// returns the original error on failure.
type ConsensusPolicy struct{}

// ShouldSwallow always returns false in production builds. The compiler
// inlines and DCEs the swallow branch at every call site.
func (ConsensusPolicy) ShouldSwallow(_ ErrorKind) bool { return false }
