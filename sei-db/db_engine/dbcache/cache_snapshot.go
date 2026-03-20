package dbcache

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// TODO before merge: write unit tests for snapshot hash behavior:
//   - SetHash / GetHash / AwaitHash happy paths
//   - SetHash errors: disabled, nil hash, double set, boot snapshot
//   - GC gating: versions not GC'd until hash is set (when tracking enabled)
//   - Boot hash loading: found in DB, missing from empty DB, missing from non-empty DB
//   - AwaitHash with context cancellation

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

// SetHash implements [CacheSnapshot].
func (s *cacheSnapshot) SetHash(hash []byte) error {
	return s.parentCache.SetSnapshotHash(s.version, hash)
}

// GetHash implements [CacheSnapshot].
func (s *cacheSnapshot) GetHash() ([]byte, error) {
	return s.parentCache.GetSnapshotHash(s.version)
}

// AwaitHash implements [CacheSnapshot].
func (s *cacheSnapshot) AwaitHash(ctx context.Context) ([]byte, error) {
	return s.parentCache.AwaitSnapshotHash(ctx, s.version)
}
