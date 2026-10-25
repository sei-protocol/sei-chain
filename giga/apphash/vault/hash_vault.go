package vault

import (
	"github.com/sei-protocol/sei-chain/giga/apphash"
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
)

// HashVault stores app hash data by block height.
type HashVault interface {
	// Safe for concurrent use with each other and with every other method.
	controllertypes.PrunableStore

	// Stores records after the highest stored record.
	//
	// records must have consecutive, ascending block heights, starting one above the highest stored record. The
	// first record stored in an empty vault may have any height. Durable when it returns if FsyncOnFlush is true.
	// Not safe for concurrent use with any other method, except those of controllertypes.PrunableStore.
	Append(records []*apphash.AppHashData) error

	// Returns the lowest and highest block heights the vault knows. The block before the giga activation height,
	// whose hashes are all zeros, is known without being stored, and is the lowest until pruning removes the record
	// of the giga activation height. Not safe for concurrent use with any other method, except those of
	// controllertypes.PrunableStore.
	Bounds() (lowest uint64, highest uint64, err error)

	// Returns an iterator over the records of the known blocks from start through end, inclusive.
	//
	// Errors unless start and end are within Bounds(). Not safe for concurrent use with any other method, except
	// those of controllertypes.PrunableStore. The returned iterator may be used from any goroutine, and later calls
	// on the vault do not change what it returns. It must be closed before Close().
	Iterator(start uint64, end uint64) (apphash.AppHashIterator, error)

	// Releases the vault's resources. Safe to call more than once. Not safe for concurrent use with any other
	// method, except those of controllertypes.PrunableStore.
	Close() error
}
