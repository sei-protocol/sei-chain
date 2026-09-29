package view

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ View = (*viewImpl)(nil)

// viewImpl is the ViewManager's implementation of View: an immutable, version-pinned
// view of the data in the manager.
type viewImpl struct {
	version       uint64
	parentManager *viewManager

	// closed records whether the view's last reservation has been released, or the view abandoned.
	closed utils.CloseMarker[viewImpl]
}

func (s *viewImpl) Name() string {
	return s.parentManager.Name()
}

func (s *viewImpl) BatchGet(keys [][]byte) (map[string][]byte, error) {
	results, err := s.parentManager.BatchGetAtVersion(keys, s.version)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get: %w", err)
	}
	return results, nil
}

func (s *viewImpl) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	value, ok, err := s.parentManager.GetAtVersion(key, s.version, updateLru)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get: %w", err)
	}
	return value, ok, nil
}

func (s *viewImpl) ForEachDiff(visit func(key string, value []byte) error) error {
	if err := s.parentManager.ForEachDiffAtVersion(s.version, visit); err != nil {
		return fmt.Errorf("failed to walk diff: %w", err)
	}
	return nil
}

func (s *viewImpl) Reserve() error {
	err := s.parentManager.IncrementReferenceCount(s.version)
	if err != nil {
		return fmt.Errorf("failed to increment reference count: %w", err)
	}
	return nil
}

func (s *viewImpl) Release() error {
	lastReleased, err := s.parentManager.DecrementReferenceCount(s.version)
	if err != nil {
		// Every failure leaves the version already dropped or the manager bricked, so nothing more is
		// released through this view.
		s.closed.Close(s)
		return fmt.Errorf("failed to decrement reference count: %w", err)
	}
	if lastReleased {
		s.closed.Close(s)
	}
	return nil
}

func (s *viewImpl) Abandon() {
	s.closed.Close(s)
}

func (s *viewImpl) Finalize(writes []*proto.KVPair) error {
	return s.parentManager.FinalizeView(s.version, writes)
}

func (s *viewImpl) AwaitFlush(ctx context.Context) error {
	c := s.parentManager
	version := s.version

	c.versionLock.Lock()
	counter, ok := c.versionMap[version]
	if !ok {
		c.versionLock.Unlock()
		return fmt.Errorf("view version (%d) is no longer tracked", version)
	}
	flushCompleted := counter.flushCompleted
	c.versionLock.Unlock()

	// Cancellation only stops the wait; the flush proceeds regardless. If a completed flush
	// and a dead context are observable simultaneously, either outcome may be returned.
	select {
	case <-flushCompleted:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before flush of version (%d): %w", version, ctx.Err())
	case <-c.ctx.Done():
		return fmt.Errorf("view manager shut down before flush of version (%d): %w",
			version, c.shutdownError())
	}
}
