package pebbleblockdb

import (
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
)

// --- command types flowing through the pipeline ---

type cmdKind int

const (
	cmdWrite cmdKind = iota
	cmdFlush
	cmdPrune
	cmdClose
)

type command struct {
	kind cmdKind

	// cmdWrite: the KV operations to apply.
	ops *blockOps

	// cmdPrune: lowest height to keep.
	pruneKeepHeight uint64

	// cmdFlush / cmdClose: caller blocks on this channel for completion.
	done chan error
}

// blockOps is the set of Pebble KV mutations produced by serializing one BinaryBlock.
type blockOps struct {
	height  uint64
	entries []kvEntry
}

type kvEntry struct {
	key   []byte
	value []byte
}

// --- background writer ---

// Maximum batch size (in bytes) before the writer auto-commits. A single
// block may push the batch slightly over this limit, but the batch will be
// committed immediately afterward.
const maxBatchBytes = 16 << 20 // 16 MB

type writer struct {
	db      *pebble.DB
	cmdCh   chan command
	cache   *pendingCache
	stopped chan struct{}

	// Running tally of value bytes added to the current batch. Pebble's
	// Batch.Len() includes internal encoding overhead, so we track the
	// application-level payload separately for a stable threshold.
	batchBytes int
}

func newWriter(db *pebble.DB, cmdCh chan command, cache *pendingCache) *writer {
	w := &writer{
		db:      db,
		cmdCh:   cmdCh,
		cache:   cache,
		stopped: make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *writer) run() {
	defer close(w.stopped)

	batch := w.db.NewBatch()
	defer batch.Close()

	var pendingHeights []uint64

	for cmd := range w.cmdCh {
		switch cmd.kind {
		case cmdWrite:
			w.addToBatch(batch, cmd.ops)
			pendingHeights = append(pendingHeights, cmd.ops.height)

			if w.batchBytes >= maxBatchBytes {
				w.commitBatch(batch, pendingHeights)
				pendingHeights = nil
				batch.Reset()
				w.batchBytes = 0
				break
			}

			// Drain additional writes that are already queued (non-blocking).
		drain:
			for {
				select {
				case next := <-w.cmdCh:
					switch next.kind {
					case cmdWrite:
						w.addToBatch(batch, next.ops)
						pendingHeights = append(pendingHeights, next.ops.height)
						if w.batchBytes >= maxBatchBytes {
							w.commitBatch(batch, pendingHeights)
							pendingHeights = nil
							batch.Reset()
							w.batchBytes = 0
							break drain
						}
					default:
						w.commitBatch(batch, pendingHeights)
						pendingHeights = nil
						batch.Reset()
						w.batchBytes = 0
						if w.handleControl(next) {
							return
						}
						break drain
					}
				default:
					break drain
				}
			}

			if len(pendingHeights) > 0 {
				w.commitBatch(batch, pendingHeights)
				pendingHeights = nil
				batch.Reset()
				w.batchBytes = 0
			}

		default:
			if w.handleControl(cmd) {
				return
			}
		}
	}
}

// handleControl processes a non-write command. Returns true if the writer
// should exit (i.e. cmdClose was handled).
func (w *writer) handleControl(cmd command) bool {
	switch cmd.kind {
	case cmdFlush:
		// TODO: propagate errors instead of panicking.
		if err := w.db.Flush(); err != nil {
			panic(fmt.Sprintf("pebble flush failed: %v", err))
		}
		cmd.done <- nil
		return false

	case cmdPrune:
		w.executePrune(cmd.pruneKeepHeight)
		return false

	case cmdClose:
		// TODO: propagate errors instead of panicking.
		if err := w.db.Flush(); err != nil {
			panic(fmt.Sprintf("pebble flush on close failed: %v", err))
		}
		cmd.done <- nil
		return true
	}
	return false
}

func (w *writer) addToBatch(batch *pebble.Batch, ops *blockOps) {
	for _, e := range ops.entries {
		// TODO: propagate errors instead of panicking.
		if err := batch.Set(e.key, e.value, nil); err != nil {
			panic(fmt.Sprintf("pebble batch set failed: %v", err))
		}
		w.batchBytes += len(e.key) + len(e.value)
	}
}

func (w *writer) commitBatch(batch *pebble.Batch, heights []uint64) {
	if batch.Empty() {
		return
	}
	// TODO: propagate errors instead of panicking.
	if err := batch.Commit(pebble.NoSync); err != nil {
		panic(fmt.Sprintf("pebble batch commit failed: %v", err))
	}
	w.cache.evict(heights)
}

// executePrune removes all blocks with height < keepHeight.
func (w *writer) executePrune(keepHeight uint64) {
	loHeight, ok := w.getMetaHeight(metaKeyLo())
	if !ok {
		return
	}
	if keepHeight <= loHeight {
		return
	}

	hiHeight, ok := w.getMetaHeight(metaKeyHi())
	if !ok {
		return
	}

	// Cap the prune target.
	pruneUpTo := keepHeight
	if pruneUpTo > hiHeight+1 {
		pruneUpTo = hiHeight + 1
	}

	// Iterate block headers to collect secondary index keys to delete.
	batch := w.db.NewBatch()
	defer batch.Close()

	blockStart, blockEnd := blockKeyRangeForPrune(loHeight, pruneUpTo)
	iter, err := w.db.NewIter(&pebble.IterOptions{
		LowerBound: blockStart,
		UpperBound: blockEnd,
	})
	if err != nil {
		panic(fmt.Sprintf("pebble new iter for prune failed: %v", err))
	}

	for valid := iter.First(); valid; valid = iter.Next() {
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

	// DeleteRange on primary block keys and tx data keys.
	if err := batch.DeleteRange(blockStart, blockEnd, nil); err != nil {
		panic(fmt.Sprintf("pebble delete range blocks: %v", err))
	}
	txStart, txEnd := txDataKeyRangeForPrune(loHeight, pruneUpTo)
	if err := batch.DeleteRange(txStart, txEnd, nil); err != nil {
		panic(fmt.Sprintf("pebble delete range tx data: %v", err))
	}

	// Update lowest-height metadata.
	newLo := pruneUpTo
	if newLo > hiHeight {
		newLo = hiHeight
	}
	if err := batch.Set(metaKeyLo(), encodeHeightValue(newLo), nil); err != nil {
		panic(fmt.Sprintf("pebble batch set meta lo: %v", err))
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		panic(fmt.Sprintf("pebble prune commit: %v", err))
	}

	// Evict pruned heights from cache.
	var prunedHeights []uint64
	for h := loHeight; h < pruneUpTo; h++ {
		prunedHeights = append(prunedHeights, h)
	}
	w.cache.evict(prunedHeights)
}

func (w *writer) getMetaHeight(key []byte) (uint64, bool) {
	val, closer, err := w.db.Get(key)
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
