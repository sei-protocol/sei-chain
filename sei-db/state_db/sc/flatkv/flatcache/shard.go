package flatcache

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

// TODO unsafe byte-> string conversion maybe?

// A single shard of a Cache.
type shard struct {
	ctx context.Context

	// A lock to protect the shard's data.
	lock sync.Mutex

	// The data in the shard.
	data map[string]*shardEntry

	// A scheduler for asyncronous reads.
	readScheduler *readScheduler
}

// The status of a value in the cache.
type valueStatus int

const (
	// The value is not known and we are not currently attempting to find it.
	statusUnknown valueStatus = iota
	// We've scheduled a read of the value but haven't yet finsihed the read.
	statusScheduled
	// The data is available.
	statusAvailable
	// We are aware that the value is deleted (special case of data being avialable).
	statusDeleted
)

// A single shardEntry in a shard. Records data for a single key.
type shardEntry struct {
	// The parent shard that contains this entry.
	shard *shard

	// The current status of this entry.
	status valueStatus

	// The value, if known.
	value []byte

	// If the value is not available when we request it,
	// it will be written to this channel when it is available.
	valueChan chan []byte
}

// Creates a new Shard.
func NewShard(readScheduler *readScheduler) *shard {
	return &shard{
		readScheduler: readScheduler,
		lock:          sync.Mutex{},
	}
}

// Get returns the value for the given key, or (nil, false) if not found.
func (s *shard) Get(key []byte) ([]byte, bool, error) {
	s.lock.Lock()

	entry := s.getEntry(key)

	switch entry.status {

	case statusAvailable:
		value := entry.value
		s.lock.Unlock()
		return value, true, nil
	case statusDeleted:
		s.lock.Unlock()
		return nil, false, nil
	case statusScheduled:
		// Another goroutine initiated a read, wait for that read to finish.
		valueChan := entry.valueChan
		s.lock.Unlock()
		value := <-valueChan
		valueChan <- value // reload the channel in case there are other listeners
		return value, value != nil, nil
	case statusUnknown:
		// We are the first goroutine to read this value.
		entry.status = statusScheduled
		valueChan := make(chan []byte, 1)
		entry.valueChan = valueChan
		s.readScheduler.ScheduleRead(key, entry)
		s.lock.Unlock()
		value := <-valueChan
		valueChan <- value // reload the channel in case there are other listeners
		return value, value != nil, nil
	default:
		panic(fmt.Sprintf("unexpected statustatus: %#v", entry.status))
	}
}

// This method is called by the read scheduler when a value becomes available.
func (se *shardEntry) InjectValue(value []byte) {
	se.shard.lock.Lock()

	if value == nil {
		se.status = statusDeleted
	} else {
		se.status = statusAvailable
		se.value = value
	}

	se.shard.lock.Unlock()
	
	se.valueChan <- value
}

// Get a shard entry for a given key. Caller is responsible for holding the shard's lock
// when this method is hcalled.
func (s *shard) getEntry(key []byte) *shardEntry {
	entry, ok := s.data[string(key)]
	if !ok {
		entry = &shardEntry{
			shard:  s,
			status: statusUnknown,
		}
		s.data[string(key)] = entry

		// TODO register with GC queue
	}

	return entry
}

// GetPrevious returns the value for the given key, or (nil, false) if not found.
// This will only return a value that is different than the current value returned by Get()
// if the cache is dirty, i.e. if there is data that has not yet been flushed down into the underlying storage.
// In the case where the cache is not dirty, this method will return the same value as Get().
func (s *shard) GetPrevious(key []byte) ([]byte, bool, error) {
	panic("unimplemented")
}

// Set sets the value for the given key.
func (s *shard) Set(key []byte, value []byte) error {
	panic("unimplemented")
}

// BatchSet sets the values for a batch of keys.
func (s *shard) BatchSet(entries []*proto.KVPair) error {
	panic("unimplemented")
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) error {
	panic("unimplemented")
}

// RunGarbageCollection runs the garbage collection process.
func (s *shard) RunGarbageCollection() error {
	panic("unimplemented")
}
