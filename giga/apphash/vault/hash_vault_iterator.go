package vault

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ apphash.AppHashIterator = (*hashVaultIterator)(nil)

// An iterator over the records of a StandardHashVault.
type hashVaultIterator struct {
	inner seiwal.Iterator[[]byte]

	// Reports the iterator if it is leaked without being closed.
	closed utils.CloseMarker[hashVaultIterator]

	// The record at the current position, or nil before the first call to Next() and after the last.
	entry *apphash.AppHashData
}

// Wraps inner, which iterates over serialized records indexed by block height.
func newHashVaultIterator(inner seiwal.Iterator[[]byte]) *hashVaultIterator {
	it := &hashVaultIterator{inner: inner}
	it.closed = utils.MustClose(it, "hash vault iterator")
	return it
}

func (it *hashVaultIterator) Next() (bool, error) {
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
	if err := it.inner.Close(); err != nil {
		return fmt.Errorf("failed to close hash vault iterator: %w", err)
	}
	return nil
}
