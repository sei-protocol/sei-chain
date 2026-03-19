package dbcache

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ CacheSnapshot = (*cacheSnapshot)(nil)

// A snapshot of the data in the cache.
type cacheSnapshot struct {
	version     uint64
	parentCache *cache
}

// BatchGet implements [CacheSnapshot].
func (s *cacheSnapshot) BatchGet(keys map[string]types.BatchGetResult) error {
	err := s.parentCache.BatchGetAtVersion(keys, s.version)
	if err != nil {
		return fmt.Errorf("failed to batch get: %w", err)
	}
	return nil
}

// Get implements [CacheSnapshot].
func (s *cacheSnapshot) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	value, ok, err := s.parentCache.GetAtVersion(key, s.version, updateLru)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get: %w", err)
	}
	return value, ok, nil
}

// GetDiff implements [CacheSnapshot].
func (s *cacheSnapshot) GetDiff() (map[string][]byte, error) {
	diff, err := s.parentCache.GetDiffAtVersion(s.version)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}
	return diff, nil
}

// Reserve implements [CacheSnapshot].
func (s *cacheSnapshot) Reserve() error {
	err := s.parentCache.IncrementReferenceCount(s.version)
	if err != nil {
		return fmt.Errorf("failed to increment reference count: %w", err)
	}
	return nil
}

// Release implements [CacheSnapshot].
func (s *cacheSnapshot) Release() error {
	err := s.parentCache.DecrementReferenceCount(s.version)
	if err != nil {
		return fmt.Errorf("failed to decrement reference count: %w", err)
	}
	return nil
}
