package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWALIterator = (*stateWALIterator)(nil)

// An iterator over the blocks of a state WAL, which computes a block's hash only when asked for it.
type stateWALIterator struct {
	// The iterator over the serialized blocks.
	serializedBlocks seiwal.Iterator[[]byte]

	// The block at the iterator's current position.
	blockNumber uint64

	// The serialization of the block at the iterator's current position.
	serialized []byte

	// The changesets of the block at the iterator's current position.
	changeSets []*proto.NamedChangeSet
}

// Next advances to the next block and reads its changesets.
func (it *stateWALIterator) Next() (bool, error) {
	ok, err := it.serializedBlocks.Next()
	if err != nil {
		return false, fmt.Errorf("failed to read the next block: %w", err)
	}
	if !ok {
		return false, nil
	}
	blockNumber, serialized := it.serializedBlocks.Entry()
	changeSets, err := deserializeBlock(serialized)
	if err != nil {
		return false, fmt.Errorf("failed to read block %d: %w", blockNumber, err)
	}
	it.blockNumber = blockNumber
	it.serialized = serialized
	it.changeSets = changeSets
	return true, nil
}

// Entry returns the block at the iterator's current position and its changesets.
func (it *stateWALIterator) Entry() (uint64, []*proto.NamedChangeSet) {
	return it.blockNumber, it.changeSets
}

// Close releases the iterator over the serialized blocks.
func (it *stateWALIterator) Close() error {
	if err := it.serializedBlocks.Close(); err != nil {
		return fmt.Errorf("failed to close the WAL iterator: %w", err)
	}
	return nil
}

// GetHash returns the hash of the block at the iterator's current position.
func (it *stateWALIterator) GetHash() ([32]byte, error) {
	return computePlaceholderHash(it.serialized), nil
}
