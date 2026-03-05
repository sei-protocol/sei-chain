package flatcache

import "github.com/sei-protocol/sei-chain/sei-iavl/proto"

// Cache describes a cache kapable of being used by a FlatKV store.
type Cache interface {

	// TODO decide if we should support individual modifications

	// Get returns the value for the given key, or (nil, false) if not found.
	Get(key []byte) ([]byte, bool, error)

	// Set sets the value for the given key.
	Set(key []byte, value []byte)

	// Delete deletes the value for the given key.
	Delete(key []byte)

	// BatchSet sets the values for a batch of keys.
	BatchSet(entries []*proto.KVPair)
}
