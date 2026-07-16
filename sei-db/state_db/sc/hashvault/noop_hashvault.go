package hashvault

import "context"

var _ HashVault = (*NoopHashVault)(nil)

// NoopHashVault is a HashVault implementation that does nothing. It provides no equivocation
// protection whatsoever. It exists for two purposes:
//   - tests that construct a BlockExecutor but do not exercise the vault, and
//   - the explicit, operator-opted-in "hash-vault-disabled-unsafe" escape hatch.
//
// Production code must never substitute this for a real vault without a deliberate human decision.
type NoopHashVault struct{}

// NewNoopHashVault returns a HashVault whose methods are all no-ops.
func NewNoopHashVault() *NoopHashVault {
	return &NoopHashVault{}
}

func (n *NoopHashVault) CommitToHash(_ context.Context, _ uint64, _ []byte) error {
	return nil
}

func (n *NoopHashVault) Prune(_ context.Context, _ uint64) error {
	return nil
}

func (n *NoopHashVault) Close(_ context.Context) error {
	return nil
}
