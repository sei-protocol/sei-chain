package flatcache

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// Cache describes a cache kapable of being used by a FlatKV store.
type Cache interface {

	// TODO decide if we should support individual modifications

	// Get returns the value for the given key, or (nil, false) if not found.
	Get(
		// The entry to fetch.
		key []byte,
		// If true, the LRU queue will be updated. If false, the LRU queue will not be updated.
		// Useful for when an operation is performed multiple times in close succession on the same key,
		// since it requires non-zero overhead to do so with little benefit.
		updateLru bool,
	) ([]byte, bool, error)

	// Set sets the value for the given key.
	Set(key []byte, value []byte)

	// Delete deletes the value for the given key.
	Delete(key []byte)

	// BatchSet applies the given changesets to the cache.
	BatchSet(cs []*proto.NamedChangeSet)
}
