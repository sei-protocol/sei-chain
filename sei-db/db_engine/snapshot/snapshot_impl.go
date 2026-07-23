package snapshot

import (
	"context"
	"fmt"
)

var _ Snapshot = (*snapshotImpl)(nil)

// snapshotImpl is the SnapshotEngine's implementation of Snapshot: an immutable, version-pinned
// view of the data in the engine.
type snapshotImpl struct {
	version      uint64
	parentEngine *snapshotEngine
}

func (s *snapshotImpl) BatchGet(keys [][]byte) (map[string][]byte, error) {
	results, err := s.parentEngine.BatchGetAtVersion(keys, s.version)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get: %w", err)
	}
	return results, nil
}

func (s *snapshotImpl) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	value, ok, err := s.parentEngine.GetAtVersion(key, s.version, updateLru)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get: %w", err)
	}
	return value, ok, nil
}

func (s *snapshotImpl) GetDiff() (map[string][]byte, error) {
	diff, err := s.parentEngine.GetDiffAtVersion(s.version)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}
	return diff, nil
}

func (s *snapshotImpl) Reserve() error {
	err := s.parentEngine.IncrementReferenceCount(s.version)
	if err != nil {
		return fmt.Errorf("failed to increment reference count: %w", err)
	}
	return nil
}

func (s *snapshotImpl) Release() error {
	err := s.parentEngine.DecrementReferenceCount(s.version)
	if err != nil {
		return fmt.Errorf("failed to decrement reference count: %w", err)
	}
	return nil
}

func (s *snapshotImpl) SetHash(hash []byte) error {
	return s.parentEngine.SetSnapshotHash(s.version, hash)
}

func (s *snapshotImpl) AwaitHash(ctx context.Context) ([]byte, error) {
	return s.parentEngine.AwaitSnapshotHash(ctx, s.version)
}

func (s *snapshotImpl) Iterator() Iterator {
	iter, err := s.parentEngine.requestIterator(s.version)
	if err != nil {
		return &errIterator{err: fmt.Errorf("failed to build snapshot iterator: %w", err)}
	}
	return iter
}

func (s *snapshotImpl) AwaitFlush(ctx context.Context) error {
	c := s.parentEngine
	version := s.version

	c.versionLock.Lock()
	counter, ok := c.versionMap[version]
	if !ok {
		c.versionLock.Unlock()
		return fmt.Errorf("snapshot version (%d) is no longer tracked", version)
	}
	flushCompleted := counter.flushCompleted
	c.versionLock.Unlock()

	select {
	case <-flushCompleted:
		return nil
	case <-ctx.Done():
		if c.isVersionFlushed(version) {
			return nil
		}
		return fmt.Errorf("context cancelled before flush of version (%d): %w", version, ctx.Err())
	case <-c.ctx.Done():
		if c.isVersionFlushed(version) {
			return nil
		}
		return fmt.Errorf("snapshot engine shut down before flush of version (%d): %w", version, c.ctx.Err())
	}
}
