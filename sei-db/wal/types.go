package wal

// MarshalFn is a function that serializes an entry to bytes.
type MarshalFn[T any] func(entry T) ([]byte, error)

// UnmarshalFn is a function that deserializes bytes to an entry.
type UnmarshalFn[T any] func(data []byte) (T, error)

// GenericWAL is a generic write-ahead log interface.
type GenericWAL[T any] interface {
	// Write will append a new entry to the end of the log.
	Write(entry T) error

	// CheckError check the error signal of async writes
	CheckError() error

	// TruncateBefore will remove all entries that are before the provided `offset`
	TruncateBefore(offset uint64) error

	// TruncateAfter will remove all entries that are after the provided `offset`
	TruncateAfter(offset uint64) error

	// ReadAt will read the replay log at the given index
	ReadAt(offset uint64) (T, error)

	// FirstOffset returns the first written index of the log
	FirstOffset() (offset uint64, err error)

	// LastOffset returns the last written index of the log
	LastOffset() (offset uint64, err error)

	// Replay will read the replay the log and process each entry with the provided function
	Replay(start uint64, end uint64, processFn func(index uint64, entry T) error) error

	Close() error
}

type GenericWALProcessor[T any] interface {
	// Start starts the subscriber processing goroutine
	Start()

	// ProcessEntry will process a new entry either sync or async
	ProcessEntry(entry T) error

	// Close will close the subscriber and stop the goroutine
	Close() error
}
