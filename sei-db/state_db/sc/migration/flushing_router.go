package migration

import (
	"errors"
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	db "github.com/tendermint/tm-db"
)

var _ Router = (*flushingRouter)(nil)

// flushingRouter wraps an inner Router and, after each successful
// ApplyChangeSets, runs a fixed list of post-apply callbacks. All other Router
// methods delegate straight to the inner router.
type flushingRouter struct {
	inner Router

	// afterApply runs in order after a successful inner.ApplyChangeSets.
	afterApply []func() error
}

// newFlushingRouter wraps inner so that afterApply callbacks run, in order,
// after each successful ApplyChangeSets.
func newFlushingRouter(inner Router, afterApply ...func() error) *flushingRouter {
	return &flushingRouter{inner: inner, afterApply: afterApply}
}

// ApplyChangeSets dispatches to the inner router and, on success, runs the
// post-apply callbacks in order. If the inner dispatch fails the callbacks are
// not run: an ApplyChangeSets error is fatal per the Router contract, and the
// accumulators' buffers are discarded with the process on the ensuing shutdown.
func (f *flushingRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error {
	if err := f.inner.ApplyChangeSets(changesets, firstBatchInBlock); err != nil {
		return err
	}
	collected := make([]error, 0, len(f.afterApply))
	for _, cb := range f.afterApply {
		if err := cb(); err != nil {
			collected = append(collected, err)
		}
	}
	if err := errors.Join(collected...); err != nil {
		return fmt.Errorf("flushingRouter: post-apply flush failed: %w", err)
	}
	return nil
}

func (f *flushingRouter) Read(store string, key []byte) ([]byte, bool, error) {
	return f.inner.Read(store, key)
}

func (f *flushingRouter) Iterator(store string, start []byte, end []byte, ascending bool) (db.Iterator, error) {
	return f.inner.Iterator(store, start, end, ascending)
}

func (f *flushingRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return f.inner.GetProof(store, key)
}
