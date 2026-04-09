package keymap

import (
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
)

// KeymapDirectoryName is the name of the directory where the keymap stores its files. One keymap directory is
// created per table
const KeymapDirectoryName = "keymap"

// KeymapDataDirectoryName is the name of the directory where the keymap implementation stores its data files.
// This directory will be created inside the keymap directory.
const KeymapDataDirectoryName = "data"

// KeymapInitializedFileName is the name of the file that indicates that the keymap has been initialized.
// This file contains no data, and serves as a flag that is set when the keymap has been fully initialized.
const KeymapInitializedFileName = "initialized"

// Keymap maintains a mapping between keys and addresses. Implementations of this interface are goroutine safe.
type Keymap interface {
	// Put adds keys to the keymap as a batch. This method is required to store the address, but can ignore
	// other fields in the ScopedKey struct such as the value length.
	//
	// A keymap provides atomicity for individual key-address pairs, but not necessarily for the batch as a whole.
	// Keys are written in batch order, meaning that if interrupted by a crash, any keys that survive will have
	// been written before any keys that did not survive.
	//
	// It is not safe to modify the contents of any slices passed to this function after the call.
	// This includes the byte slices containing the keys.
	Put(pairs []types.ScopedKey) error

	// Get returns the address for a key. Returns true if the key exists, and false otherwise (i.e. does not
	// return an error if the key does not exist).
	//
	// It is not safe to modify key byte slice after it is passed to this method.
	Get(key []byte) (address types.Address, exists bool, err error)

	// Delete removes keys from the keymap. Deleting non-existent keys is a no-op.
	//
	// Deletion of keys is atomic, but deletion is not atomic across the entire batch.
	//
	// It is not safe to modify the contents of any slices passed to this function after the call.
	// This includes the byte slices containing the keys.
	Delete(keys []types.ScopedKey) error

	// Stop stops the keymap.
	Stop() error

	// Destroy stops the keymap and permanently deletes all data.
	Destroy() error

	// ReverseIterator returns a reverse iterator that walks from the most recently written key
	// backwards through the linked list embedded in keymap values. Used during crash recovery.
	//
	// It is not safe to use a ReverseIterator concurrently with other methods on the same Keymap.
	ReverseIterator() (KeymapReverseIterator, error)
}

// KeymapReverseIterator walks the keymap in reverse write order by following a linked list
// embedded in the values. Each value contains a pointer to the previously written key.
// Iteration stops when the chain is exhausted (prevKey is nil) or when a link is missing
// (the referenced key has been deleted). If values are deleted out of order (e.g. by GC
// deleting a middle segment), iteration halts at the boundary of the first missing link.
// This is safe because GC always deletes in write order (oldest first), so the chain from
// the head backward to the oldest non-deleted key is always intact.
//
// A ReverseIterator is not safe for concurrent use, and must not be used concurrently with
// other methods on the same Keymap.
type KeymapReverseIterator interface {
	// Next returns the next key and address walking backwards through the chain.
	// Returns exists=false when the chain is exhausted or a link is missing.
	Next() (key []byte, address types.Address, exists bool, err error)

	// Delete removes the current entry (the one most recently returned by Next) from the keymap.
	Delete() error

	// Close releases iterator resources.
	Close() error
}

// BuildKeymap is a function that builds a Keymap.
type BuildKeymap func(logger *slog.Logger, keymapPath string, doubleWriteProtection bool) (Keymap, bool, error)

// emptyReverseIterator is a KeymapReverseIterator that immediately reports exhaustion.
// Returned when a keymap has no entries to iterate.
type emptyReverseIterator struct{}

func (e *emptyReverseIterator) Next() ([]byte, types.Address, bool, error) {
	return nil, types.Address{}, false, nil
}

func (e *emptyReverseIterator) Delete() error {
	return fmt.Errorf("no current entry to delete")
}

func (e *emptyReverseIterator) Close() error {
	return nil
}
