package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWALIterator = (*stateWALIterator)(nil)

// An iterator over the blocks of a state WAL, which computes a block's hash only when asked for it.
type stateWALIterator struct {
	// The iterator over the stored records.
	records seiwal.Iterator[stateWALRecord]

	// The block at the iterator's current position.
	blockNumber uint64

	// The record of the block at the iterator's current position.
	record stateWALRecord

	// The changesets of the block at the iterator's current position.
	changeSets []*proto.NamedChangeSet
}

// Next advances to the next block and reads its changesets.
func (it *stateWALIterator) Next() (bool, error) {
	ok, err := it.records.Next()
	if err != nil {
		return false, fmt.Errorf("failed to read the next record: %w", err)
	}
	if !ok {
		return false, nil
	}
	blockNumber, record := it.records.Entry()
	changeSets, err := record.toChangeSets()
	if err != nil {
		return false, fmt.Errorf("failed to read block %d: %w", blockNumber, err)
	}
	it.blockNumber = blockNumber
	it.record = record
	it.changeSets = changeSets
	return true, nil
}

// Entry returns the block at the iterator's current position and its changesets.
func (it *stateWALIterator) Entry() (uint64, []*proto.NamedChangeSet) {
	return it.blockNumber, it.changeSets
}

// Close releases the iterator over the stored records.
func (it *stateWALIterator) Close() error {
	if err := it.records.Close(); err != nil {
		return fmt.Errorf("failed to close the record iterator: %w", err)
	}
	return nil
}

// GetHash returns the hash of the block at the iterator's current position.
func (it *stateWALIterator) GetHash() ([32]byte, error) {
	// STOPGAP: recomputes the hash from the block's record. The real BUD starts from the BUD tree, or hashes will
	// be stored in the WAL and read back. See computePlaceholderHash().
	return it.record.hash(), nil
}
