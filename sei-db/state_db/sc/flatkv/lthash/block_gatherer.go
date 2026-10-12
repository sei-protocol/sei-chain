package lthash

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// blockGatherer reads what each sealed block changed and submits its leaf hashing to the pool.
type blockGatherer struct {
	// hasher fans this block's leaf hashing out across the pool.
	hasher *leafHasher

	// Sealed blocks and control messages arrive here from ScheduleHash().
	scheduledBlockChan chan any

	// Once a block has been gathered, it is put onto this channel for the combiner, in block order.
	combineJobChan chan any

	// Cancelled when the engine is stopping, to release a send that the combiner is no longer reading.
	ctx context.Context

	// cancel stops the engine, called when run() returns so that a caller waiting on this queue is
	// released.
	cancel context.CancelFunc

	// brick latches a failure on the engine, which reports it from Close().
	brick func(error)

	// wg tracks run(), so that the engine can wait for it to return.
	wg sync.WaitGroup
}

func newBlockGatherer(
	cfg *Config,
	hasher *leafHasher,
	// Cancelled when the engine is stopping, to release a send the combiner is no longer reading.
	ctx context.Context,
	// Stops the engine, called when run() returns.
	cancel context.CancelFunc,
	// Latches a failure on the engine, which reports it from Close().
	brick func(error),
) *blockGatherer {
	g := &blockGatherer{
		hasher:             hasher,
		scheduledBlockChan: make(chan any, cfg.ScheduleQueueSize),
		combineJobChan:     make(chan any, cfg.CombineQueueSize),
		ctx:                ctx,
		cancel:             cancel,
		brick:              brick,
	}
	g.wg.Go(g.run)
	return g
}

// run reads each block's changed values, submits its leaf hashing to the pool, and passes the block to
// the combiner. It stops the engine on the way out, whatever the reason: a schedule waits under the
// engine's context, and this goroutine is the only thing that can release it.
func (g *blockGatherer) run() {
	defer g.teardown()
	// Cancelled before the drain rather than after it, so that a schedule parked on a full queue is
	// released by the cancellation instead of being woken by the drain, which nothing follows.
	defer g.cancel()

	for {
		select {
		case message := <-g.scheduledBlockChan:
			switch request := message.(type) {
			case *hashRequest:
				if !g.gather(request) {
					return
				}
			case *flushRequest:
				if !g.forward(request) {
					return
				}
			default:
				g.brick(fmt.Errorf("unknown engine message type %T", message))
				return
			}
		case <-g.ctx.Done():
			return
		}
	}
}

// Drain the queue without hashing it and stop the combiner.
func (g *blockGatherer) teardown() {
	defer close(g.combineJobChan)
	g.discardQueued()
}

// discardQueued empties the queue without hashing it, releasing each block's reservation. It runs when
// the gatherer stops, and from the engine's enqueue() for a message that reaches the queue after that.
func (g *blockGatherer) discardQueued() {
	for {
		select {
		case message := <-g.scheduledBlockChan:
			request, ok := message.(*hashRequest)
			if !ok {
				continue
			}
			if err := request.release(); err != nil {
				g.brick(fmt.Errorf("release block %d while stopping: %w", request.blockNumber, err))
			}
		default:
			return
		}
	}
}

// Deal with one block from the gatherer's queue, reporting whether the gatherer may carry on.
func (g *blockGatherer) gather(request *hashRequest) bool {
	changed, err := gatherChangesFromAllStores(request.current, request.previous)

	// Released even when the read failed: a reservation left held stalls its database's flushes
	// indefinitely, and the read's own failure is reported either way.
	releaseErr := request.release()
	if err == nil {
		err = releaseErr
	}

	var hashes leafHashes
	if err == nil {
		hashes, err = g.hasher.submit(changed)
	}
	if err != nil {
		err = fmt.Errorf("gather block %d: %w", request.blockNumber, err)
	}

	return g.forward(&gatheredBlock{
		blockNumber: request.blockNumber,
		hashes:      hashes,
		err:         err,
	})
}

// forward hands one message to the combiner
func (g *blockGatherer) forward(message any) bool {
	select {
	case g.combineJobChan <- message:
		return true
	case <-g.ctx.Done():
		return false
	}
}

// Gather changes from all stores.
func gatherChangesFromAllStores(current *sview.StoreView, previous *sview.StoreView) ([]DatabaseMutations, error) {
	out := make([]DatabaseMutations, 4)
	errs := make([]error, 4)

	var wg sync.WaitGroup
	wg.Go(func() { out[0], errs[0] = gatherChangesFromStore(current.AccountView(), previous.AccountView()) })
	wg.Go(func() { out[1], errs[1] = gatherChangesFromStore(current.CodeView(), previous.CodeView()) })
	wg.Go(func() { out[2], errs[2] = gatherChangesFromStore(current.StorageView(), previous.StorageView()) })
	wg.Go(func() { out[3], errs[3] = gatherChangesFromStore(current.MiscView(), previous.MiscView()) })
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

// Gather the changes from a specific store.
func gatherChangesFromStore(current view.View, previous view.View) (DatabaseMutations, error) {
	// One pass over the view's writes. The key is copied because a mutation outlives the walk, while
	// the value is the view's own and stays valid until the view retires, which is after hashing.
	var mutations []KeyMutation
	err := current.ForEachDiff(func(key string, value []byte) error {
		if strings.HasPrefix(key, ktype.MetaKeyPrefix) {
			return nil
		}
		mutations = append(mutations, KeyMutation{
			Key:    []byte(key),
			Value:  value,
			Delete: value == nil,
		})
		return nil
	})
	if err != nil {
		return DatabaseMutations{}, fmt.Errorf("%s read diff: %w", current.Name(), err)
	}
	if len(mutations) == 0 {
		return DatabaseMutations{DBName: current.Name()}, nil
	}
	if previous == nil {
		return DatabaseMutations{DBName: current.Name(), Mutations: mutations}, nil
	}

	// Aliases the keys already held by the mutations rather than copying them again.
	changedKeys := make([][]byte, 0, len(mutations))
	for i := range mutations {
		changedKeys = append(changedKeys, mutations[i].Key)
	}
	old, err := previous.BatchGet(changedKeys)
	if err != nil {
		return DatabaseMutations{}, fmt.Errorf("%s read previous values: %w", current.Name(), err)
	}
	for i := range mutations {
		mutations[i].LastValue = old[string(mutations[i].Key)]
	}
	return DatabaseMutations{DBName: current.Name(), Mutations: mutations}, nil
}
