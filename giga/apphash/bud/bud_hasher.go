package bud

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// BUDHasher computes the BUD hash of each block submitted to it, and reports the hashes in submission order.
type BUDHasher interface {
	// Submit hands over a block's mutations to be hashed, and may block while the hasher is behind.
	Submit(
		ctx context.Context,
		// The height of the block the mutations belong to.
		blockHeight uint64,
		// The block's mutations, one per key the block writes, in any order. Retained along with the bytes they
		// reference, so neither may be modified after the call.
		mutations []mutation.Mutation,
	) error

	// Flush returns once the BUD hash of every block submitted before the call has been reported.
	Flush(ctx context.Context) error

	// Close reports the BUD hash of every block submitted before the call, then stops the hasher. A block
	// submitted concurrently with the call may or may not be reported.
	Close() error
}
