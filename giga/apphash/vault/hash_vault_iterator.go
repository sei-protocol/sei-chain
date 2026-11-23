package vault

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ apphash.AppHashIterator = (*hashVaultIterator)(nil)

// An iterator over the records of a StandardHashVault.
type hashVaultIterator struct {
	// The stored records, or nil if the iterator returns none.
	inner seiwal.Iterator[[]byte]

	// Reports the iterator if it is leaked without being closed.
	closed utils.CloseMarker[hashVaultIterator]

	// The record at the current position, or nil before the first call to Next() and after the last.
	entry *apphash.AppHashData

	// The records the next calls to Next() return, in order, before reading inner.
	pending []*apphash.AppHashData
}

// Wraps inner, which iterates over serialized records indexed by block height. Errors, closing inner, unless the
// first record inner yields is for block start.
func newHashVaultIterator(inner seiwal.Iterator[[]byte], start uint64) (*hashVaultIterator, error) {
	it := &hashVaultIterator{inner: inner}
	it.closed = utils.MustClose(it, "hash vault iterator")
	first, err := it.readFirst(start)
	if err != nil {
		return nil, errors.Join(err, it.Close())
	}
	it.pending = []*apphash.AppHashData{first}
	return it, nil
}

// Returns an iterator over zeroHash alone.
func newZeroHashIterator(zeroHash *apphash.AppHashData) *hashVaultIterator {
	it := &hashVaultIterator{pending: []*apphash.AppHashData{zeroHash}}
	it.closed = utils.MustClose(it, "hash vault iterator")
	return it
}

// Prepend makes record the next one Next() returns.
func (it *hashVaultIterator) Prepend(record *apphash.AppHashData) {
	it.pending = append([]*apphash.AppHashData{record}, it.pending...)
}

// Reads the first record from inner, which must be for block start.
func (it *hashVaultIterator) readFirst(start uint64) (*apphash.AppHashData, error) {
	ok, err := it.read()
	if err != nil {
		return nil, fmt.Errorf("failed to read the hash vault record of block %d: %w", start, err)
	}
	if !ok {
		return nil, fmt.Errorf("no hash vault record is stored for block %d", start)
	}
	if it.entry.BlockHeight() != start {
		// The WAL starts an iterator whose start is below its first record at that first record.
		return nil, fmt.Errorf("no hash vault record is stored for block %d, the first one above it is for "+
			"block %d", start, it.entry.BlockHeight())
	}
	first := it.entry
	it.entry = nil
	return first, nil
}

func (it *hashVaultIterator) Next() (bool, error) {
	if len(it.pending) > 0 {
		it.entry = it.pending[0]
		it.pending = it.pending[1:]
		return true, nil
	}
	if it.inner == nil {
		it.entry = nil
		return false, nil
	}
	return it.read()
}

// Advances inner to its next record and decodes it into entry.
func (it *hashVaultIterator) read() (bool, error) {
	it.entry = nil
	ok, err := it.inner.Next()
	if err != nil {
		return false, fmt.Errorf("failed to read hash vault: %w", err)
	}
	if !ok {
		return false, nil
	}
	index, data := it.inner.Entry()
	record, err := deserializeRecord(data)
	if err != nil {
		return false, fmt.Errorf("failed to decode hash vault record at block %d: %w", index, err)
	}
	if record.BlockHeight() != index {
		return false, fmt.Errorf("hash vault record stored at block %d is for block %d",
			index, record.BlockHeight())
	}
	it.entry = record
	return true, nil
}

func (it *hashVaultIterator) Entry() *apphash.AppHashData {
	return it.entry
}

func (it *hashVaultIterator) Close() error {
	it.closed.Close(it)
	it.entry = nil
	it.pending = nil
	if it.inner == nil {
		return nil
	}
	if err := it.inner.Close(); err != nil {
		return fmt.Errorf("failed to close hash vault iterator: %w", err)
	}
	return nil
}
