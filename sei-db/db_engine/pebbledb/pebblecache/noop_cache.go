package pebblecache

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ Cache = (*noOpCache)(nil)

// noOpCache is a Cache that performs no caching. Every Get falls through
// to the underlying readFunc. Set, Delete, and BatchSet are no-ops.
// Useful for testing the storage layer without cache interference, or for
// workloads where caching is not beneficial.
type noOpCache struct {
	readFunc func(key []byte) ([]byte, bool, error)
}

// NewNoOpCache creates a Cache that always reads from readFunc and never caches.
func NewNoOpCache(readFunc func(key []byte) ([]byte, bool, error)) Cache {
	return &noOpCache{readFunc: readFunc}
}

func (c *noOpCache) Get(key []byte, _ bool) ([]byte, bool, error) {
	return c.readFunc(key)
}

func (c *noOpCache) BatchGet(keys map[string]types.BatchGetResult) error {
	var firstErr error
	for k := range keys {
		val, _, err := c.readFunc([]byte(k))
		if err != nil {
			keys[k] = types.BatchGetResult{Error: err}
			if firstErr == nil {
				firstErr = err
			}
		} else {
			keys[k] = types.BatchGetResult{Value: val}
		}
	}
	if firstErr != nil {
		return fmt.Errorf("unable to batch get: %w", firstErr)
	}
	return nil
}

func (c *noOpCache) Set([]byte, []byte) {
	// intentional no-op
}

func (c *noOpCache) Delete([]byte) {
	// intentional no-op
}

func (c *noOpCache) BatchSet([]CacheUpdate) error {
	// intentional no-op
	return nil
}
