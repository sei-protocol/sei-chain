package vault

import "github.com/sei-protocol/sei-chain/giga/apphash"

// HashVault stores app hash data by block height. It is not safe for concurrent use, except as Iterator()
// describes.
type HashVault interface {

	// Stores records after the highest stored record.
	//
	// records must have consecutive, ascending block heights, starting one above the highest stored record. The
	// first record stored in an empty vault may have any height. Durable when it returns if FsyncOnFlush is true.
	Append(records []*apphash.AppHashData) error

	// Returns the lowest and highest stored block heights, with ok false if the vault is empty.
	Bounds() (ok bool, lowest uint64, highest uint64, err error)

	// Returns an iterator over the stored records from start through end, inclusive.
	//
	// Errors if start is below the lowest stored record or end is above the highest. The iterator may be used
	// from any goroutine, and later calls on the vault do not change what it returns. It must be closed before
	// Close().
	Iterator(start uint64, end uint64) (apphash.AppHashIterator, error)

	// Permits deleting the records below blockHeight.
	//
	// They may be deleted at any later time.
	Prune(blockHeight uint64) error

	// Releases the vault's resources. Safe to call more than once.
	Close() error
}
