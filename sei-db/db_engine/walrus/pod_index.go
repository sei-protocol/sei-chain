package walrus

// PodIndex is the handle for one pod's index.
//
// The index answers where a key's versions live inside the pod, in time logarithmic in the number of
// distinct keys the pod holds.
//
// It is two files. The searched one holds a hash per distinct key and is mapped; the other holds the keys
// themselves and their versions, and is read only once a search has landed on an entry. Path reports the pod
// directory both live in.
//
// A PodIndex is safe for concurrent use: a written index never changes.
type PodIndex interface {

	// FindNewest returns where the newest version of key written in the half-open block range
	// (lowBlock, highBlock] lives.
	//
	// The range is half-open at the bottom because a walk stops at a snapshot: versions at or below the
	// snapshot's block are answered by the snapshot itself, not by the pod that happens to also hold them.
	// Pass a lowBlock below the pod's first block to search the whole pod.
	FindNewest(key []byte, lowBlock uint64, highBlock uint64) (
		// The byte offset of the entry in the pod's data section. Only valid if found is true.
		offset uint32,
		// The block that wrote the entry. Only valid if found is true.
		blockNumber uint64,
		// Whether the pod holds a version of key in the range.
		found bool,
		// Whether the pod holds key at all, in any block. This is separate from found because a pod can hold
		// a key whose every version falls outside the range, and the bloom filter that admitted the pod was
		// right to. Counting that as a false positive would overstate the filter's error rate.
		present bool,
		// Any error encountered while searching.
		err error,
	)

	// Path returns the pod directory the index's files live in.
	Path() string

	// Size returns the bytes the index's files occupy together.
	Size() int64

	// Delete removes the index's files.
	Delete() error
}
