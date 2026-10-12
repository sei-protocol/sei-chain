package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWALIterator = (*stateWALIterator)(nil)

// An iterator over the blocks of a state WAL, which computes a block's hash only when asked for it.
type stateWALIterator struct {
	seiwal.Iterator[[]*proto.NamedChangeSet]
}

// GetHash returns the hash of the block at the iterator's current position.
func (it *stateWALIterator) GetHash() ([32]byte, error) {
	// STOPGAP: recomputes the hash by marshaling the block again. The real BUD starts from the BUD tree, or hashes
	// will be stored in the WAL and read back. See computePlaceholderHash().
	blockNumber, cs := it.Entry()
	hash, err := computePlaceholderHash(cs)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to compute the hash of block %d: %w", blockNumber, err)
	}
	return hash, nil
}
