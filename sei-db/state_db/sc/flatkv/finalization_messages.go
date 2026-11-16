package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// The messages a FinalizationManager accepts. They share one queue so that a request is answered in the
// order it was made relative to the blocks around it.

// pendingFinalization is one sealed block awaiting its hash.
type pendingFinalization struct {
	// blockHeight is the height being finalized. Checked against the hash that arrives for it, since the
	// two streams are independent and a mismatch means one of them has slipped.
	blockHeight uint64

	// storeView is the block's view of every store, with a reservation this manager owns. Held until the
	// block's hashes have been written onto it, because a view's last release must follow its finalization.
	storeView *sview.StoreView

	// viewsToFinalize is the views this block's hashes are recorded onto.
	viewsToFinalize []view.View
}

// Releases the reservation this block holds, so its databases can resume flushing.
func (p *pendingFinalization) release() error {
	return p.storeView.Release()
}

// finalizationFlushRequest asks the manager to report once it has dealt with everything queued ahead of
// it.
type finalizationFlushRequest struct {
	// done is closed once every message queued ahead of this one has been dealt with. A channel rather
	// than a value, so the manager answering it can never block on a caller that has given up.
	doneChan chan struct{}
}

func newFinalizationFlushRequest() *finalizationFlushRequest {
	return &finalizationFlushRequest{doneChan: make(chan struct{})}
}
