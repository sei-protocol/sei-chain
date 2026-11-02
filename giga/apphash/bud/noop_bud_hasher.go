package bud

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

var _ BUDHasher = (*noOpBUDHasher)(nil)

// noOpBUDHasher is a BUDHasher that hashes and reports nothing.
type noOpBUDHasher struct{}

// NewNoOpBUDHasher returns a BUDHasher that hashes and reports nothing.
func NewNoOpBUDHasher() BUDHasher {
	return &noOpBUDHasher{}
}

func (n *noOpBUDHasher) Submit(context.Context, uint64, []mutation.Mutation) error {
	// intentional no-op
	return nil
}

func (n *noOpBUDHasher) Close() error {
	// intentional no-op
	return nil
}
