package builder

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
)

// A position in a read of consecutive stored app hashes, which serves reads of blocks too old for the recent app hash
// cache. Owned by the builder goroutine.
type vaultReadCursor struct {

	// Reads the stored records after current.
	iterator apphash.AppHashIterator

	// The record read most recently.
	current *apphash.AppHashData

	// The last block iterator reads.
	end uint64

	// When the cursor last served a read.
	lastServed time.Time

	// How long the cursor may go without serving a read before it should be retired.
	idleTimeout time.Duration
}

// Opens a cursor over the stored records of blocks start through end, positioned on block start.
func openVaultReadCursor(
	hashVault vault.HashVault,
	start uint64,
	end uint64,
	// How long the cursor may go without serving a read before it should be retired.
	idleTimeout time.Duration,
) (*vaultReadCursor, error) {
	iterator, err := hashVault.Iterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to read stored app hashes from block %d: %w", start, err)
	}
	first, err := nextStoredRecord(iterator, start)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to read the stored app hash of block %d: %w", start, err),
			iterator.Close())
	}
	return &vaultReadCursor{
		iterator:    iterator,
		current:     first,
		end:         end,
		lastServed:  time.Now(),
		idleTimeout: idleTimeout,
	}, nil
}

// CanRead reports whether Read() can return blockHeight: the block the cursor is on, or the one after it.
func (c *vaultReadCursor) CanRead(blockHeight uint64) bool {
	current := c.current.BlockHeight()
	return blockHeight == current || (blockHeight == current+1 && current < c.end)
}

// Read returns the stored record of blockHeight, which CanRead() must allow, and moves the cursor onto it. After
// an error, only Close() may be called.
func (c *vaultReadCursor) Read(blockHeight uint64) (*apphash.AppHashData, error) {
	c.lastServed = time.Now()
	if blockHeight == c.current.BlockHeight() {
		return c.current, nil
	}
	record, err := nextStoredRecord(c.iterator, blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to read the stored app hash of block %d: %w", blockHeight, err)
	}
	c.current = record
	return record, nil
}

// ShouldBeRetired reports whether the cursor has gone its idle timeout without serving a read.
func (c *vaultReadCursor) ShouldBeRetired() bool {
	return time.Since(c.lastServed) >= c.idleTimeout
}

// Exhausted reports whether the cursor is on the last block it reads.
func (c *vaultReadCursor) Exhausted() bool {
	return c.current.BlockHeight() == c.end
}

// Close releases the cursor's iterator.
func (c *vaultReadCursor) Close() error {
	if err := c.iterator.Close(); err != nil {
		return fmt.Errorf("failed to close the stored app hash iterator: %w", err)
	}
	return nil
}
