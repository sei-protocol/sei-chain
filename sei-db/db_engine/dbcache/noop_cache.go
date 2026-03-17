package dbcache

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ Cache = (*noOpCache)(nil)

// noOpCache is a Cache that performs no caching. Every Get falls through
// to the provided Reader. Set, Delete, and BatchSet are no-ops.
// Useful for testing the storage layer without cache interference, or for
// workloads where caching is not beneficial.
type noOpCache struct{}

// NewNoOpCache creates a Cache that always reads via the provided Reader and never caches.
func NewNoOpCache() Cache {
	return &noOpCache{}
}

func (c *noOpCache) Get(read Reader, key []byte, _ bool) ([]byte, bool, error) {
	return read(key)
}

func (c *noOpCache) BatchGet(read Reader, keys map[string]types.BatchGetResult) error {
	var firstErr error
	for k := range keys {
		val, _, err := read([]byte(k))
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
