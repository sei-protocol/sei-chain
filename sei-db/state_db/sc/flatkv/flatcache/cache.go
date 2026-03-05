package flatcache

import "github.com/sei-protocol/sei-chain/sei-iavl/proto"

// Cache describes a cache kapable of being used by a FlatKV store.
type Cache interface {

	// TODO decide if we should support individual modifications

	// Get returns the value for the given key, or (nil, false) if not found.
	Get(key []byte) ([]byte, bool, error)

	// GetPrevious returns the value for the given key, or (nil, false) if not found.
	// This will only return a value that is different than the current value returned by Get()
	// if the cache is dirty, i.e. if there is data that has not yet been flushed down into the underlying storage.
	// In the case where the cache is not dirty, this method will return the same value as Get().
	GetPrevious(key []byte) ([]byte, bool, error)

	// Set sets the value for the given key.
	Set(key []byte, value []byte) error

	// Delete deletes the value for the given key.
	Delete(key []byte) error

	// BatchSet sets the values for a batch of keys.
	BatchSet(entries []*proto.KVPair) error
}
