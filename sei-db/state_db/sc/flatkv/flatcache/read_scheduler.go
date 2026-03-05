package flatcache

import (
	"context"
	"fmt"
)

// A utility for scheduling asyncronous DB reads.
type readScheduler struct {
	ctx         context.Context
	readFunc    func(key []byte) []byte
	requestChan chan *readRequest
}

// A request to read a value from the database.
type readRequest struct {
	// The key to read.
	key []byte

	// The entry to write the result to.
	entry *shardEntry
}

// Creates a new ReadScheduler.
func NewReadScheduler(
	ctx context.Context,
	readFunc func(key []byte) []byte,
	// The number of background goroutines to read values from the database.
	workerCount int,
	// The max size of the read queue.
	readQueueSize int,
) *readScheduler {
	rs := &readScheduler{
		ctx:         ctx,
		readFunc:    readFunc,
		requestChan: make(chan *readRequest, readQueueSize),
	}

	for i := 0; i < workerCount; i++ {
		go rs.readWorker()
	}

	return rs
}

// ScheduleRead schedules a read for the given key within the given shard.
// This method returns immediately, and the read is performed asynchronously.
// When eventually completed, the read result is inserted into the provided shard entry
func (r *readScheduler) ScheduleRead(key []byte, entry *shardEntry) error {
	select {
	case <-r.ctx.Done():
		return fmt.Errorf("context done")
	case r.requestChan <- &readRequest{key: key, entry: entry}:
		return nil
	}
}

// A worker that reads values from the database.
func (r *readScheduler) readWorker() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case request := <-r.requestChan:
			value := r.readFunc(request.key)
			request.entry.InjectValue(request.key, value)
		}
	}
}
