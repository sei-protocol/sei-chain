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
	// Latches a failure on the engine, which reports it from Close().
	brick func(error),
) *blockGatherer {
	g := &blockGatherer{
		hasher:             hasher,
		scheduledBlockChan: make(chan any, cfg.ScheduleQueueSize),
		combineJobChan:     make(chan any, cfg.CombineQueueSize),
		ctx:                ctx,
		brick:              brick,
	}
	g.wg.Go(g.run)
	return g
}

// run reads each block's changed values, submits its leaf hashing to the pool, and passes the block to
// the combiner.
func (g *blockGatherer) run() {
	defer g.teardown()

	for {
		select {
		case message := <-g.scheduledBlockChan:
			switch request := message.(type) {
			case *hashRequest:
				g.gather(request)
			case *flushRequest:
				g.combineJobChan <- request
			default:
				g.brick(fmt.Errorf("unknown engine message type %T", message))
				return
			}
		case <-g.ctx.Done():
			return
		}
	}
}

// Drain the queue without hashing it, releasing each block's reservation.
func (g *blockGatherer) teardown() {
	defer close(g.combineJobChan)

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

// Deal with one block from the gatherer's queue.
func (g *blockGatherer) gather(request *hashRequest) {
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

	g.combineJobChan <- &gatheredBlock{
		blockNumber: request.blockNumber,
		hashes:      hashes,
		err:         err,
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
	diff, err := current.GetDiff()
	if err != nil {
		return DatabaseMutations{}, fmt.Errorf("%s read diff: %w", current.Name(), err)
	}
	if len(diff) == 0 {
		return DatabaseMutations{DBName: current.Name()}, nil
	}

	changedKeys := make([][]byte, 0, len(diff))
	for key := range diff {
		if strings.HasPrefix(key, ktype.MetaKeyPrefix) {
			continue
		}
		changedKeys = append(changedKeys, []byte(key))
	}
	if len(changedKeys) == 0 {
		return DatabaseMutations{DBName: current.Name()}, nil
	}

	var old map[string][]byte
	if previous != nil {
		if old, err = previous.BatchGet(changedKeys); err != nil {
			return DatabaseMutations{}, fmt.Errorf("%s read previous values: %w", current.Name(), err)
		}
	}

	out := make([]KeyMutation, 0, len(changedKeys))
	for _, key := range changedKeys {
		value := diff[string(key)]
		out = append(out, KeyMutation{
			Key:       key,
			Value:     value,
			LastValue: old[string(key)],
			Delete:    value == nil,
		})
	}
	return DatabaseMutations{DBName: current.Name(), Mutations: out}, nil
}
