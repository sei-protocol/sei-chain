package walrus

// PodBloom is the handle for one pod's bloom filter file.
//
// It is the coarse level of the search: a walk tests each pod's filter and searches the pod's index only when
// the filter does not rule the key out.
//
// The handle holds metadata, never the filter's bits. Every pod's handle stays resident, and at petabyte
// scale the bits would run to terabytes.
//
// A PodBloom is safe for concurrent use: a written filter never changes.
type PodBloom interface {

	// MayContain reports whether the pod may hold key. False is definitive. True may be a false positive, at
	// roughly the rate the filter was sized for.
	MayContain(key []byte) bool

	// Path returns the file the filter lives in.
	Path() string

	// Size returns the bytes the file occupies.
	Size() int64

	// Delete removes the file.
	Delete() error
}
