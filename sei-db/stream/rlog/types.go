package rlog

import "github.com/sei-protocol/sei-db/proto"

type Writer interface {
	// Write will write a new entry to the log at the given index.
	Write(entry LogEntry) error

	// CheckAsyncCommit check the error signal of async writes
	CheckAsyncCommit() error

	// WaitAsyncCommit will block and wait for async writes to complete
	WaitAsyncCommit() error

	// TruncateBefore will remove all entries that are before the provided `index`
	TruncateBefore(index uint64) error

	// TruncateAfter will remove all entries that are after the provided `index`
	TruncateAfter(index uint64) error
}

type Reader interface {
	// ReadAt will read the replay log at the given index
	ReadAt(index uint64) (*proto.ReplayLogEntry, error)
	// Replay will read the replay log and process each log entry with the provided function
	Replay(start uint64, end uint64, processFn func(index uint64, entry proto.ReplayLogEntry) error) error

	// StartSubscriber starts the underline subscriber goroutine to keep read the replay log from a given index
	StartSubscriber(fromIndex uint64, processFn func(index uint64, entry proto.ReplayLogEntry) error)

	// StopSubscriber stops the underline subscriber goroutine
	StopSubscriber() error

	// CheckSubscriber check the error signal of the subscriber
	CheckSubscriber() error
}

type LogEntry struct {
	Index uint64
	Data  proto.ReplayLogEntry
}
