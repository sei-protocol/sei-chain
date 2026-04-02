package pebbleblockdb

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/cockroachdb/pebble/v2"
)

// pruner runs in its own goroutine, independent of the write pipeline.
// Prune requests are coalesced into a single atomic high-water mark,
// so rapid-fire Prune() calls (one per block in steady state) collapse
// into a single batch operation.
type pruner struct {
	db       *pebble.DB
	cache    *pendingCache
	loHeight *atomic.Uint64 // shared with pebbleBlockDB

	// High-water mark: prune everything below this height.
	target atomic.Uint64

	// Last height we actually pruned up to, tracked locally to skip no-op wakes.
	lastDone uint64

	wakeCh  chan struct{}      // non-blocking signal to check for work
	syncCh  chan chan struct{} // blocking: execute pending, then ack
	stopCh  chan struct{}      // signal to shut down
	stopped chan struct{}      // closed when goroutine exits
}

func newPruner(db *pebble.DB, cache *pendingCache, loHeight *atomic.Uint64) *pruner {
	pr := &pruner{
		db:       db,
		cache:    cache,
		loHeight: loHeight,
		wakeCh:   make(chan struct{}, 1),
		syncCh:   make(chan chan struct{}),
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go pr.run()
	return pr
}

func (pr *pruner) run() {
	defer close(pr.stopped)
	for {
		select {
		case <-pr.wakeCh:
			pr.executePending()
		case ack := <-pr.syncCh:
			pr.executePending()
			close(ack)
		case <-pr.stopCh:
			pr.executePending()
			return
		}
	}
}

// wake sends a non-blocking signal to the pruner to check for work.
func (pr *pruner) wake() {
	select {
	case pr.wakeCh <- struct{}{}:
	default:
	}
}

// sync blocks until all pending prune work is complete.
func (pr *pruner) sync() {
	ack := make(chan struct{})
	pr.syncCh <- ack
	<-ack
}

// stop signals the pruner to execute any remaining work and exit.
func (pr *pruner) stop() {
	close(pr.stopCh)
	<-pr.stopped
}

func (pr *pruner) executePending() {
	target := pr.target.Load()
	if target <= pr.lastDone {
		return
	}

	loHeight, ok := pr.getMetaHeight(metaKeyLo())
	if !ok {
		return
	}
	if target <= loHeight {
		pr.lastDone = target
		return
	}

	hiHeight, ok := pr.getMetaHeight(metaKeyHi())
	if !ok {
		return
	}

	pruneUpTo := target
	if pruneUpTo > hiHeight+1 {
		pruneUpTo = hiHeight + 1
	}

	batch := pr.db.NewBatch()
	defer batch.Close()

	blockStart, blockEnd := blockKeyRangeForPrune(loHeight, pruneUpTo)
	iter, err := pr.db.NewIter(&pebble.IterOptions{
		LowerBound: blockStart,
		UpperBound: blockEnd,
	})
	if err != nil {
		panic(fmt.Sprintf("pebble new iter for prune failed: %v", err))
	}

	var prunedHeights []uint64
	for valid := iter.First(); valid; valid = iter.Next() {
		height := decodeBlockKeyHeight(iter.Key())
		prunedHeights = append(prunedHeights, height)

		hdr, err := unmarshalBlockHeader(iter.Value())
		if err != nil {
			panic(fmt.Sprintf("unmarshal block header during prune: %v", err))
		}
		if err := batch.Delete(encodeHashIdxKey(hdr.hash), nil); err != nil {
			panic(fmt.Sprintf("pebble batch delete hash idx: %v", err))
		}
		for _, txHash := range hdr.txHashes {
			if err := batch.Delete(encodeTxIdxKey(txHash), nil); err != nil {
				panic(fmt.Sprintf("pebble batch delete tx idx: %v", err))
			}
		}
	}
	if err := iter.Close(); err != nil {
		panic(fmt.Sprintf("pebble iter close: %v", err))
	}

	if err := batch.DeleteRange(blockStart, blockEnd, nil); err != nil {
		panic(fmt.Sprintf("pebble delete range blocks: %v", err))
	}
	txStart, txEnd := txDataKeyRangeForPrune(loHeight, pruneUpTo)
	if err := batch.DeleteRange(txStart, txEnd, nil); err != nil {
		panic(fmt.Sprintf("pebble delete range tx data: %v", err))
	}

	newLo := pruneUpTo
	if newLo > hiHeight {
		newLo = hiHeight
	}
	if err := batch.Set(metaKeyLo(), encodeHeightValue(newLo), nil); err != nil {
		panic(fmt.Sprintf("pebble batch set meta lo: %v", err))
	}

	// TODO: propagate errors instead of panicking.
	if err := batch.Commit(pebble.NoSync); err != nil {
		panic(fmt.Sprintf("pebble prune commit: %v", err))
	}

	pr.cache.evict(prunedHeights)
	pr.loHeight.Store(newLo)
	pr.lastDone = target
}

func (pr *pruner) getMetaHeight(key []byte) (uint64, bool) {
	val, closer, err := pr.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, false
		}
		panic(fmt.Sprintf("pebble get meta: %v", err))
	}
	h := decodeHeightValue(val)
	closer.Close()
	return h, true
}
