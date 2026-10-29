package hashvault

import "context"

var _ HashVault = (*NoopHashVault)(nil)

// NoopHashVault is a HashVault implementation that does nothing. It provides no equivocation
// protection whatsoever. It exists for tests that construct a BlockExecutor but do not exercise
// the vault.
//
// Production code must never substitute this for a real vault.
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
