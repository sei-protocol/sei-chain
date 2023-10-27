package stream

type Stream[T any] interface {
	// Write will write a new entry to the log at the given index.
	Write(offset uint64, entry T) error

	// CheckAsyncCommit check the error signal of async writes
	CheckAsyncCommit() error

	// Flush will block and wait for async writes to complete
	Flush() error

	// TruncateBefore will remove all entries that are before the provided `offset`
	TruncateBefore(offset uint64) error

	// TruncateAfter will remove all entries that are after the provided `offset`
	TruncateAfter(offset uint64) error

	// ReadAt will read the replay log at the given index
	ReadAt(offset uint64) (*T, error)

	// LastOffset returns the last written index of the replay log
	LastOffset() (offset uint64, err error)

	// Replay will read the replay log and process each log entry with the provided function
	Replay(start uint64, end uint64, processFn func(index uint64, entry T) error) error

	Close() error
}
