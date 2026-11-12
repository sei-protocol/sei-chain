package memiavl

import (
	"sync/atomic"
	"time"
)

// readAheadWindowSize is the number of bytes of a snapshot's files that a read-ahead window spans.
const readAheadWindowSize = 3 << 30

// readAheadWindows reads a snapshot's files into the page cache one window at a time, the first time
// an ascending iterator reaches each window. A window is a run of consecutive leaves together with the
// stretches of the nodes and kvs files that an ascending walk over those leaves reads.
type readAheadWindows struct {
	tree     string
	snapshot *Snapshot
	// leaves is the number of leaves in the snapshot, and windowLeaves the number in each window but
	// the last.
	leaves, windowLeaves uint64
	// read records the windows an iterator has reached.
	read []atomic.Bool
}

// armReadAhead makes the ascending iterators created on the snapshot until the returned function is
// called read its files into the page cache in windows of about windowSize bytes. It does nothing when
// the snapshot has no leaves or already has a read-ahead armed.
func (snapshot *Snapshot) armReadAhead(tree string, windowSize uint64) (stop func()) {
	leaves := uint64(snapshot.leavesLen()) //nolint:gosec
	if leaves == 0 {
		return func() {}
	}
	size := uint64(len(snapshot.nodes)) + uint64(len(snapshot.leaves)) + uint64(len(snapshot.kvs))
	windowLeaves := max(1, windowSize/(size/leaves))
	windows := &readAheadWindows{
		tree:         tree,
		snapshot:     snapshot,
		leaves:       leaves,
		windowLeaves: windowLeaves,
		read:         make([]atomic.Bool, (leaves+windowLeaves-1)/windowLeaves),
	}
	if !snapshot.readAhead.CompareAndSwap(nil, windows) {
		return func() {}
	}
	logger.Info("reading tree into page cache ahead of iterators",
		"tree", tree, "size-mib", size>>20, "window-mib", windowSize>>20, "windows", len(windows.read))
	return func() { snapshot.readAhead.CompareAndSwap(windows, nil) }
}

// reach reads the window holding leaf into the page cache unless an iterator reached it before, and
// returns the index of the first leaf past that window. The caller must hold a reference on the
// snapshot.
func (w *readAheadWindows) reach(leaf uint32) uint64 {
	window := uint64(leaf) / w.windowLeaves
	from := window * w.windowLeaves
	to := min(from+w.windowLeaves, w.leaves)
	if w.read[window].CompareAndSwap(false, true) {
		started := time.Now()
		size := w.snapshot.fillPageCacheForLeaves(from, to)
		logger.Info("read tree window into page cache", "tree", w.tree, "window", window+1,
			"windows", len(w.read), "size-mib", size>>20, "elapsed-sec", time.Since(started).Seconds())
	}
	return to
}

// fillPageCacheForLeaves reads into the page cache the stretches of the snapshot's files that an
// ascending walk over leaves [from, to) reads, and returns their combined size in bytes.
func (snapshot *Snapshot) fillPageCacheForLeaves(from, to uint64) uint64 {
	spans := [...]struct {
		file     *MmapFile
		from, to uint64
	}{
		{snapshot.leavesMap, from * SizeLeaf, to * SizeLeaf},
		// A branch node is stored just below the index of the last leaf under it, so the walk reads
		// the branch nodes at the same offsets of the nodes file, which holds one record fewer.
		{snapshot.nodesMap, from * SizeNode, min(to*SizeNode, uint64(len(snapshot.nodes)))},
		{snapshot.kvsMap, snapshot.kvsOffset(from), snapshot.kvsOffset(to)},
	}
	var size uint64
	for _, span := range spans {
		if span.to <= span.from {
			continue
		}
		fillPageCache(span.file.file, int64(span.from), int64(span.to-span.from)) //nolint:gosec
		size += span.to - span.from
	}
	return size
}

// kvsOffset returns the offset in the kvs file of the record of leaf, or the size of the file for the
// index past the last leaf.
func (snapshot *Snapshot) kvsOffset(leaf uint64) uint64 {
	if leaf >= uint64(snapshot.leavesLen()) { //nolint:gosec
		return uint64(len(snapshot.kvs))
	}
	return snapshot.leavesLayout.Leaf(uint32(leaf)).KeyOffset() //nolint:gosec
}
