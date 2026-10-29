package statewal

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
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

	// The mutations of the block at the iterator's current position.
	mutations []mutation.Mutation
}

// Next advances to the next block and reads its mutations.
func (it *stateWALIterator) Next() (bool, error) {
	ok, err := it.serializedBlocks.Next()
	if err != nil {
		return false, fmt.Errorf("failed to read the next block: %w", err)
	}
	if !ok {
		return false, nil
	}
	blockNumber, serialized := it.serializedBlocks.Entry()
	mutations, err := deserializeBlock(serialized)
	if err != nil {
		return false, fmt.Errorf("failed to read block %d: %w", blockNumber, err)
	}
	cloneMutationBytes(mutations)
	it.blockNumber = blockNumber
	it.serialized = serialized
	it.mutations = mutations
	return true, nil
}

// cloneMutationBytes replaces every byte slice in mutations with a copy of its own, preserving nil.
//
// Decoded mutations point into the buffer holding the whole WAL file. A caller that retained one value would keep
// that entire buffer alive, so each value gets its own allocation instead.
func cloneMutationBytes(mutations []mutation.Mutation) {
	for i := range mutations {
		mutations[i].Key = bytes.Clone(mutations[i].Key)
		mutations[i].PreviousValue = bytes.Clone(mutations[i].PreviousValue)
		mutations[i].NewValue = bytes.Clone(mutations[i].NewValue)
	}
}

// Entry returns the block at the iterator's current position and its mutations.
func (it *stateWALIterator) Entry() (uint64, []mutation.Mutation) {
	return it.blockNumber, it.mutations
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
	hash, err := hashSerializedBlock(it.serialized)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to hash block %d: %w", it.blockNumber, err)
	}
	return hash, nil
}
