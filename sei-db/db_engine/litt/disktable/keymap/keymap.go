package keymap

import (
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
	// The batch must be applied atomically with respect to crash recovery: the entire batch either becomes
	// durable as a unit or not at all. After a crash, recovery must never observe a batch that was only
	// partially applied. Keymap repair relies on this guarantee: it rescues keys missing from the keymap in
	// a single Put, and a partially-durable batch would leave a hole that subsequent repairs cannot detect.
	//
	// It is not safe to modify the contents of any slices passed to this function after the call.
	// This includes the byte slices containing the keys.
	Put(pairs []*types.ScopedKey) error

	// Get returns the address for a key. Returns true if the key exists, and false otherwise (i.e. does not
	// return an error if the key does not exist).
	//
	// It is not safe to modify key byte slice after it is passed to this method.
	Get(key []byte) (types.Address, bool, error)

	// Delete removes keys from the keymap. Deleting non-existent keys is a no-op.
	//
	// Deletion of keys is atomic, but deletion is not atomic across the entire batch.
	//
	// It is not safe to modify the contents of any slices passed to this function after the call.
	// This includes the byte slices containing the keys.
	Delete(keys []*types.ScopedKey) error

	// Stop stops the keymap.
	Stop() error

	// Destroy stops the keymap and permanently deletes all data.
	Destroy() error
}

// BuildKeymap is a function that builds a Keymap.
type BuildKeymap func(logger *slog.Logger, keymapPath string, doubleWriteProtection bool) (Keymap, bool, error)
