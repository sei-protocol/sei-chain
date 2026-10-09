package statewal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// budListenerRegistry holds the listeners each block's BUD is delivered to, and the most recent BUD they were
// given.
//
// Every method is safe to call from any goroutine.
type budListenerRegistry struct {
	// Guards every field below as one. A registration that interleaved with a delivery would either miss a block
	// or be told the wrong block to expect next.
	mu sync.Mutex

	// The listeners, in registration order.
	listeners []BUDListener

	// Whether lastBlockNumber and lastBUD are set.
	hasLast bool

	// The block of the most recent BUD delivered, or of the last stored block when the WAL was opened.
	lastBlockNumber uint64

	// The BUD of lastBlockNumber.
	lastBUD [32]byte
}

// A block whose BUD the BUD goroutine computes and delivers to the listeners.
type budBlock struct {
	// The block number.
	blockNumber uint64

	// The changesets written for the block.
	cs []*proto.NamedChangeSet
}

// Asks the BUD goroutine to close done once every block sent before it has been delivered.
type budFlush struct {
	// Closed by the BUD goroutine when it reaches this request.
	done chan struct{}
}

// computePlaceholderBUD returns the BUD of a block's changesets: SHA-256 over each changeset's protobuf encoding,
// each prefixed by its length as a uvarint.
//
// STOPGAP: this is a placeholder, intentionally not a BUD as the BUD spec defines it.
func computePlaceholderBUD(cs []*proto.NamedChangeSet) ([32]byte, error) {
	// This placeholder exists so the API that delivers BUDs can be built and run on a private testnet before the
	// real BUD lands. It is replaced wholesale then. Its known shortcuts, all deliberate:
	//
	//   - Protobuf encoding is not deterministic by specification. It is deterministic in practice for a fixed
	//     protobuf library version and compiler, which a private testnet can pin across every node. A public chain
	//     cannot pin every validator's build, and nodes on different builds could disagree on a block's BUD, so
	//     this must not ship there.
	//   - Every block is marshaled twice, once for its WAL record and once here.
	//   - The iterator recomputes a block's BUD by marshaling it again. The real BUD starts from the BUD tree, or
	//     BUDs will be stored in the WAL and read back rather than recomputed.
	//   - The input is its own encoding rather than the WAL record. The BUD is a function of the changesets, not of
	//     how they are stored, so a change to the record format must not change any block's BUD.
	h := sha256.New()
	var lengthPrefix [binary.MaxVarintLen64]byte
	for i, ncs := range cs {
		if ncs == nil {
			return [32]byte{}, fmt.Errorf("changeset at index %d is nil", i)
		}
		marshaled, err := ncs.Marshal()
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to marshal changeset at index %d: %w", i, err)
		}
		n := binary.PutUvarint(lengthPrefix[:], uint64(len(marshaled)))
		_, _ = h.Write(lengthPrefix[:n]) // hash.Hash.Write never returns an error
		_, _ = h.Write(marshaled)
	}
	var bud [32]byte
	h.Sum(bud[:0])
	return bud, nil
}

// newBUDListenerRegistry returns a registry with no listeners and no BUD delivered.
func newBUDListenerRegistry() *budListenerRegistry {
	return &budListenerRegistry{}
}

// seed sets the BUD reported to listeners registered before any delivery.
func (r *budListenerRegistry) seed(
	// The block the BUD belongs to.
	blockNumber uint64,
	// The BUD of the block.
	bud [32]byte,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hasLast = true
	r.lastBlockNumber = blockNumber
	r.lastBUD = bud
}

// register adds listener, unless it is nil, and returns the most recent BUD delivered before it was added, or the
// seeded BUD when none has been. ok is false when there is neither.
func (r *budListenerRegistry) register(listener BUDListener) (ok bool, blockNumber uint64, bud [32]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if listener != nil {
		r.listeners = append(r.listeners, listener)
	}
	return r.hasLast, r.lastBlockNumber, r.lastBUD
}

// deliver hands one block's BUD to every listener, returning the first listener error.
func (r *budListenerRegistry) deliver(
	// Passed to each listener.
	ctx context.Context,
	// The block the BUD belongs to.
	blockNumber uint64,
	// The BUD of the block.
	bud [32]byte,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, listener := range r.listeners {
		if err := listener(ctx, blockNumber, bud); err != nil {
			return fmt.Errorf("a BUD listener refused block %d: %w", blockNumber, err)
		}
	}
	r.hasLast = true
	r.lastBlockNumber = blockNumber
	r.lastBUD = bud
	return nil
}

// budLoop computes each written block's BUD and delivers it to the listeners, in the order blocks were sent, and
// answers flush requests in that same order. Runs on its own goroutine until the WAL stops; a failure bricks the
// WAL.
func (w *stateWALImpl) budLoop() {
	defer w.wg.Done()
	for {
		var msg any
		select {
		case <-w.ctx.Done():
			return
		case msg = <-w.budChan:
		}

		switch m := msg.(type) {
		case budBlock:
			if err := w.deliverBUD(m); err != nil {
				_ = w.fail(err)
				return
			}
		case budFlush:
			close(m.done)
		default:
			_ = w.fail(fmt.Errorf("unknown BUD goroutine message %T", msg))
			return
		}
	}
}

// deliverBUD computes the BUD of block and delivers it to the listeners.
func (w *stateWALImpl) deliverBUD(block budBlock) error {
	// STOPGAP: the block's second marshal, after the one for its WAL record. See computePlaceholderBUD().
	bud, err := computePlaceholderBUD(block.cs)
	if err != nil {
		return fmt.Errorf("failed to compute the BUD of block %d: %w", block.blockNumber, err)
	}
	if err := w.budListeners.deliver(w.ctx, block.blockNumber, bud); err != nil {
		return fmt.Errorf("failed to deliver the BUD of block %d: %w", block.blockNumber, err)
	}
	return nil
}

// sendToBUDLoop hands msg to the BUD goroutine, blocking while its channel is full. It fails once the WAL stops.
func (w *stateWALImpl) sendToBUDLoop(msg any) error {
	select {
	case w.budChan <- msg:
		return nil
	case <-w.ctx.Done():
		return w.stoppedErr()
	}
}

// awaitBUDs blocks until every block written before the call has been delivered to the listeners. It fails once
// the WAL stops.
func (w *stateWALImpl) awaitBUDs() error {
	done := make(chan struct{})
	if err := w.sendToBUDLoop(budFlush{done: done}); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-w.ctx.Done():
		return w.stoppedErr()
	}
}
