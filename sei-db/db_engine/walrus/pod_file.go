package walrus

// PodReader is the handle for one pod's data file, the log of block changes the pod holds.
//
// The handle holds metadata, never the pod's data.
//
// A PodReader is safe for concurrent use: a written pod never changes.
type PodReader interface {

	// ReadEntry returns the value and deletion flag of the entry at the given offset into the pod's data
	// section.
	//
	// The key is not returned because the caller already has it: an offset only comes from a pod index
	// search, which confirmed the full key before naming the offset. An offset that does not name an entry
	// boundary is reported as an error rather than decoded into an arbitrary entry.
	ReadEntry(offset uint32) (value []byte, deleted bool, err error)

	// Path returns the file the pod's data lives in.
	Path() string

	// Size returns the bytes the file occupies.
	Size() int64

	// Delete removes the file.
	Delete() error
}
