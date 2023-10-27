package types

type Stream[T any] interface {
	// Write will write a new entry to the log at the given index.
	Write(offset uint64, entry T) error

	// CheckError check the error signal of async writes
	CheckError() error

	// TruncateBefore will remove all entries that are before the provided `offset`
	TruncateBefore(offset uint64) error

	// TruncateAfter will remove all entries that are after the provided `offset`
	TruncateAfter(offset uint64) error

	// ReadAt will read the replay log at the given index
	ReadAt(offset uint64) (*T, error)

	// FirstOffset returns the first written index of the log
	FirstOffset() (offset uint64, err error)

	// LastOffset returns the last written index of the log
	LastOffset() (offset uint64, err error)

	// Replay will read the replay the log and process each entry with the provided function
	Replay(start uint64, end uint64, processFn func(index uint64, entry T) error) error

	Close() error
}

type Subscriber[T any] interface {
	Start()

	ProcessEntry(entry T) error

	Close() error
}
