package view

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ View = (*viewImpl)(nil)

// viewImpl is the ViewManager's implementation of View: an immutable, block-pinned
// view of the data in the manager.
type viewImpl struct {
	blockHeight   uint64
	parentManager *viewManager

	// closed records whether the view's last reservation has been released, or the view abandoned.
	closed utils.CloseMarker[viewImpl]
}

func (v *viewImpl) Name() string {
	return v.parentManager.Name()
}

func (v *viewImpl) BatchGet(keys [][]byte) (map[string][]byte, error) {
	results, err := v.parentManager.BatchGetAtBlockHeight(keys, v.blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get: %w", err)
	}
	return results, nil
}

func (v *viewImpl) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	value, ok, err := v.parentManager.GetAtBlockHeight(key, v.blockHeight, updateLru)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get: %w", err)
	}
	return value, ok, nil
}

func (v *viewImpl) ForEachDiff(visit func(key string, value []byte) error) error {
	if err := v.parentManager.ForEachDiffAtBlockHeight(v.blockHeight, visit); err != nil {
		return fmt.Errorf("failed to walk diff: %w", err)
	}
	return nil
}

func (v *viewImpl) Reserve() error {
	err := v.parentManager.IncrementReferenceCount(v.blockHeight)
	if err != nil {
		return fmt.Errorf("failed to increment reference count: %w", err)
	}
	return nil
}

func (v *viewImpl) Release() error {
	lastReleased, err := v.parentManager.DecrementReferenceCount(v.blockHeight)
	if err != nil {
		// Every failure leaves the block already dropped or the manager bricked, so nothing more is
		// released through this view.
		v.closed.Close(v)
		return fmt.Errorf("failed to decrement reference count: %w", err)
	}
	if lastReleased {
		v.closed.Close(v)
	}
	return nil
}

func (v *viewImpl) Abandon() {
	v.closed.Close(v)
}

func (v *viewImpl) BlockHeight() uint64 {
	return v.blockHeight
}

func (v *viewImpl) Finalize(blockHeight uint64, writes []*proto.KVPair) error {
	if blockHeight != v.blockHeight {
		return fmt.Errorf("%s: cannot finalize the view of block %d as block %d",
			v.Name(), v.blockHeight, blockHeight)
	}
	return v.parentManager.FinalizeView(v.blockHeight, writes)
}

func (v *viewImpl) AwaitFlush(ctx context.Context) error {
	c := v.parentManager
	blockHeight := v.blockHeight

	c.versionLock.Lock()
	counter, ok := c.versionMap[blockHeight]
	if !ok {
		c.versionLock.Unlock()
		return fmt.Errorf("the view of block %d is no longer tracked", blockHeight)
	}
	flushCompleted := counter.flushCompleted
	c.versionLock.Unlock()

	// Cancellation only stops the wait; the flush proceeds regardless. If a completed flush
	// and a dead context are observable simultaneously, either outcome may be returned.
	select {
	case <-flushCompleted:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before flush of block (%d): %w", blockHeight, ctx.Err())
	case <-c.ctx.Done():
		return fmt.Errorf("view manager shut down before flush of block (%d): %w",
			blockHeight, c.shutdownError())
	}
}
