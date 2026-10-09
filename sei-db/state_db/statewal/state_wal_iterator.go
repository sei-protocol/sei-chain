package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWALIterator = (*stateWALIterator)(nil)

// An iterator over the blocks of a state WAL, which computes a block's BUD only when asked for it.
type stateWALIterator struct {
	seiwal.Iterator[[]*proto.NamedChangeSet]
}

// GetHash returns the BUD of the block at the iterator's current position.
func (it *stateWALIterator) GetHash() ([32]byte, error) {
	// STOPGAP: recomputes the BUD by marshaling the block again. The real BUD starts from the BUD tree, or BUDs
	// will be stored in the WAL and read back. See computePlaceholderBUD().
	blockNumber, cs := it.Entry()
	bud, err := computePlaceholderBUD(cs)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to compute the BUD of block %d: %w", blockNumber, err)
	}
	return bud, nil
}
